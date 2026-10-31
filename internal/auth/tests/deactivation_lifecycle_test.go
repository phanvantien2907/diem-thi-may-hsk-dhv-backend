package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hskvinh/backend/internal/auth"
	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/middleware"
)

func issueTestJWT(secret string, userID int64, role string) string {
	claims := jwt.MapClaims{
		"sub":       "50",
		"user_id":   userID,
		"user_role": role,
		"exp":       time.Now().Add(15 * time.Minute).Unix(),
		"iat":       time.Now().Unix(),
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	return tok
}

func TestDeactivateAccountByAdmin_ConfirmationSessionRevocationCascadeAndReactivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	const jwtSecret = "test-jwt-secret"
	svc := auth.NewService(db, &fakeAudit{}, jwtSecret, 15*time.Minute, 24*time.Hour)
	h := auth.NewHandler(svc)

	// 1. Mismatched confirmation identity must be rejected immediately
	err = svc.SoftDeleteAccountByAdmin(context.Background(), 1, 50, request.DeactivateAccountByAdminRequestDTO{
		ConfirmAccountID: 999, // wrong ID
		ConfirmIdentity:  "040123456060",
		Reason:           "Vi phạm quy chế thi",
	})
	if !errors.Is(err, auth.ErrDeactivationConfirmationMismatch) {
		t.Fatalf("expected ErrDeactivationConfirmationMismatch, got %v", err)
	}

	// 2. Valid confirmation cascades unpaid registration, disables account, revokes sessions & JWTs
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT a\.account_username, a\.account_email.+FROM accounts a.+FOR UPDATE OF a`).
		WithArgs(int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_username", "account_email", "account_full_name", "candidate_id", "candidate_full_name",
		}).AddRow("040123456060", "cand50@example.com", "Nguyễn Văn B", int64(500), "Nguyễn Văn B"))

	mock.ExpectQuery(`SELECT er\.exam_registration_id, er\.exam_registration_exam_seat_id, er\.exam_registration_status FROM exam_registrations er`).
		WithArgs(int64(500)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_id", "exam_registration_exam_seat_id", "exam_registration_status",
		}).AddRow(int64(901), int64(301), "pending_payment"))

	mock.ExpectExec(`UPDATE exam_registrations SET exam_registration_status = 'cancelled'`).
		WithArgs(int64(901)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE payments SET payment_status = 'cancelled'`).
		WithArgs(int64(901), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE exam_seats SET exam_seat_status = 'available'`).
		WithArgs(int64(301)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`UPDATE accounts SET account_status = 'disabled', account_is_deleted = TRUE`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE candidates SET candidate_is_deleted = TRUE`).
		WithArgs(int64(500)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE auth_sessions SET auth_session_revoked_at = NOW\(\)`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	// Issue a valid JWT BEFORE deactivation
	activeJWT := issueTestJWT(jwtSecret, 50, "candidate")

	err = svc.SoftDeleteAccountByAdmin(context.Background(), 1, 50, request.DeactivateAccountByAdminRequestDTO{
		ConfirmAccountID: 50,
		ConfirmIdentity:  "Nguyễn Văn B",
		Reason:           "Tài khoản trùng lặp theo yêu cầu thí sinh",
	})
	if err != nil {
		t.Fatalf("SoftDeleteAccountByAdmin() unexpected error = %v", err)
	}

	// 3. Verify that the previously issued JWT for account 50 is IMMEDIATELY rejected by middleware.JWTAuth
	r := gin.New()
	r.GET("/me", middleware.JWTAuth(jwtSecret), h.Me)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+activeJWT)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected deactivated user's JWT to be rejected with 401 Unauthorized, got %d; body: %s", w.Code, w.Body.String())
	}

	// 4. Reactivate account 50 and verify JWT revocation is cleared
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts SET account_status = 'active', account_is_deleted = FALSE`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE candidates SET candidate_is_deleted = FALSE`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.ReactivateAccountByAdmin(context.Background(), 1, 50, request.ReactivateAccountByAdminRequestDTO{
		Reason: "Khôi phục tài khoản sau khi xác minh",
	}); err != nil {
		t.Fatalf("ReactivateAccountByAdmin() error = %v", err)
	}
	if middleware.IsAccountTokenRevoked(context.Background(), 50) {
		t.Fatalf("expected account 50 JWT revocation to be cleared after reactivation")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandler_AdminDeleteAccount_RequiresConfirmationBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := auth.NewService(db, &fakeAudit{}, "secret", 15*time.Minute, 24*time.Hour)
	h := auth.NewHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(1))
	})
	r.DELETE("/admin/delete-account/:id", h.AdminDeleteAccount)

	// Calling DELETE without confirmation payload must return 400 Bad Request
	req := httptest.NewRequest(http.MethodDelete, "/admin/delete-account/50", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when confirmation payload is missing, got %d; body: %s", w.Code, w.Body.String())
	}
}

