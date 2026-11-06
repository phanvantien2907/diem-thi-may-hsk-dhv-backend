package tests

import (
	"context"
	"encoding/json"
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
	"github.com/hskvinh/backend/internal/ws"
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
			"account_username", "account_email", "account_full_name", "candidate_id", "candidate_full_name", "doc_number",
		}).AddRow("040123456060", "cand50@example.com", "Nguyễn Văn B", int64(500), "Nguyễn Văn B", "040123456060"))

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

func TestLogin_DeactivatedAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := auth.NewService(db, &fakeAudit{}, "secret", 15*time.Minute, 24*time.Hour)
	h := auth.NewHandler(svc)

	r := gin.New()
	r.POST("/api/v1/auth/login", h.Login)

	// Simulate account that has been soft-deleted / deactivated by admin (account_status = 'disabled' / 'deactivated')
	mock.ExpectQuery(`SELECT account_id, account_password_hash`).
		WithArgs("040123456060").
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "account_password_hash", "account_status",
			"account_failed_login_count", "account_locked_until", "account_must_change_password",
		}).AddRow(
			int64(50), "$2a$10$dummyhash", "deactivated", 0, nil, false,
		))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"040123456060","password":"Password123!"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for deactivated account login, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if resp.Code != "ACCOUNT_DEACTIVATED" {
		t.Fatalf("expected code ACCOUNT_DEACTIVATED, got %q", resp.Code)
	}
	expectedMsg := "Tài khoản của bạn đã bị vô hiệu hóa. Vui lòng liên hệ Quản trị viên để được hỗ trợ."
	if resp.Message != expectedMsg {
		t.Fatalf("expected message %q, got %q", expectedMsg, resp.Message)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestDeactivateAccount_ForceLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	hub := ws.NewHub()
	go hub.Run()

	// Register an active WebSocket client for candidate account 50
	wsClient := ws.NewClient(hub, nil, 50, "candidate")

	// Wait briefly for hub registration
	deadline := time.Now().Add(500 * time.Millisecond)
	for hub.UserConnectionCount(50) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.UserConnectionCount(50) != 1 {
		t.Fatalf("expected 1 active WS connection for account 50, got %d", hub.UserConnectionCount(50))
	}

	svc := auth.NewService(db, &fakeAudit{}, "secret", 15*time.Minute, 24*time.Hour)
	svc.SetHub(hub)
	h := auth.NewHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(1))
	})
	r.POST("/api/v1/admin/candidates/:id/deactivate", h.AdminDeactivateCandidate)

	// Candidate ID 500 -> resolves to account ID 50
	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(int64(500)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_account_id"}).AddRow(int64(50)))

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT a\.account_username, a\.account_email.+FROM accounts a.+FOR UPDATE OF a`).
		WithArgs(int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_username", "account_email", "account_full_name", "candidate_id", "candidate_full_name", "doc_number",
		}).AddRow("040123456060", "cand50@example.com", "Nguyễn Văn B", int64(500), "Nguyễn Văn B", "040123456060"))

	mock.ExpectQuery(`SELECT er\.exam_registration_id, er\.exam_registration_exam_seat_id, er\.exam_registration_status FROM exam_registrations er`).
		WithArgs(int64(500)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_id", "exam_registration_exam_seat_id", "exam_registration_status",
		}))

	mock.ExpectExec(`UPDATE accounts SET account_status = 'disabled', account_is_deleted = TRUE`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE candidates SET candidate_is_deleted = TRUE`).
		WithArgs(int64(500)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE auth_sessions SET auth_session_revoked_at = NOW\(\)`).
		WithArgs(int64(50)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := `{"confirm_account_id":50,"confirm_identity":"040123456060","reason":"Vi phạm quy chế thi nghiêm trọng"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/candidates/500/deactivate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from POST /api/v1/admin/candidates/:id/deactivate, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify FORCE_LOGOUT WebSocket payload was emitted to the candidate's active socket
	select {
	case rawMsg := <-wsClient.SendChan():
		var envelope struct {
			Event string `json:"event"`
			Data  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Reason  string `json:"reason"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rawMsg, &envelope); err != nil {
			t.Fatalf("failed to unmarshal WS message: %v", err)
		}
		if ws.EventType(envelope.Event) != ws.EventForceLogout {
			t.Fatalf("expected WS event %q, got %q", ws.EventForceLogout, envelope.Event)
		}
		if envelope.Data.Code != "ACCOUNT_DEACTIVATED" {
			t.Fatalf("expected WS payload code ACCOUNT_DEACTIVATED, got %q", envelope.Data.Code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for FORCE_LOGOUT WebSocket event")
	}

	// Verify active WebSocket connection was terminated and removed from Hub
	if hub.UserConnectionCount(50) != 0 {
		t.Fatalf("expected 0 active WS connections for account 50 after deactivation, got %d", hub.UserConnectionCount(50))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}


