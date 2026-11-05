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
	"github.com/hskvinh/backend/internal/auth"
	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

func TestAdminPin_SetupAndVerify_Issues5MinScopedToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	const jwtSecret = "test-pii-gateway-secret"
	const adminID int64 = 1
	svc := auth.NewService(db, &fakeAudit{}, jwtSecret, 15*time.Minute, 24*time.Hour)

	// 1. Reject invalid PIN format (non-6-digit)
	if err := svc.SetupAdminPin(context.Background(), adminID, request.SetupAdminPinRequestDTO{Pin: "12ab56"}); !errors.Is(err, auth.ErrInvalidPinFormat) {
		t.Fatalf("expected ErrInvalidPinFormat for non-numeric PIN, got %v", err)
	}

	// 2. First-time PIN setup (account_pii_pin_hash is NULL)
	passHash, _ := utils.HashPassword("Admin@123456")
	mock.ExpectQuery(`SELECT account_password_hash, account_pii_pin_hash FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_password_hash", "account_pii_pin_hash"}).
			AddRow(passHash, nil))

	mock.ExpectExec(`UPDATE accounts SET account_pii_pin_hash = \$1`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.SetupAdminPin(context.Background(), adminID, request.SetupAdminPinRequestDTO{Pin: "290726"}); err != nil {
		t.Fatalf("SetupAdminPin() unexpected error = %v", err)
	}

	// 3. Verify PIN -> issues 5-minute pii_access_token with scope="pii_access"
	pinHash, _ := utils.HashPassword("290726")
	mock.ExpectQuery(`SELECT account_pii_pin_hash, COALESCE\(account_pii_pin_failed_count, 0\), account_pii_pin_locked_until FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_pii_pin_hash", "account_pii_pin_failed_count", "account_pii_pin_locked_until"}).
			AddRow(pinHash, 2, nil))

	mock.ExpectExec(`UPDATE accounts SET account_pii_pin_failed_count = 0, account_pii_pin_locked_until = NULL`).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	res, err := svc.VerifyAdminPin(context.Background(), adminID, request.VerifyAdminPinRequestDTO{Pin: "290726"})
	if err != nil {
		t.Fatalf("VerifyAdminPin() unexpected error = %v", err)
	}
	if res.Scope != middleware.ScopePIIAccess {
		t.Fatalf("expected scope=%q, got %q", middleware.ScopePIIAccess, res.Scope)
	}
	if res.ExpiresIn != 300 {
		t.Fatalf("expected strict 5-minute TTL (300s), got %d", res.ExpiresIn)
	}
	if !middleware.VerifyPIIAccessToken(res.PiiAccessToken, jwtSecret, adminID) {
		t.Fatalf("expected VerifyPIIAccessToken to accept issued pii_access_token")
	}
	if middleware.VerifyPIIAccessToken(res.PiiAccessToken, jwtSecret, 999) {
		t.Fatalf("expected VerifyPIIAccessToken to reject mismatched admin ID")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestAdminPin_BruteForceLockout_After5FailedAttempts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	const jwtSecret = "test-pii-gateway-secret"
	const adminID int64 = 7
	svc := auth.NewService(db, &fakeAudit{}, jwtSecret, 15*time.Minute, 24*time.Hour)
	pinHash, _ := utils.HashPassword("654321")

	// Attempt #4 (failed_count goes from 3 -> 4, 1 attempt remaining)
	mock.ExpectQuery(`SELECT account_pii_pin_hash, COALESCE\(account_pii_pin_failed_count, 0\), account_pii_pin_locked_until FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_pii_pin_hash", "account_pii_pin_failed_count", "account_pii_pin_locked_until"}).
			AddRow(pinHash, 3, nil))

	mock.ExpectExec(`UPDATE accounts SET account_pii_pin_failed_count = \$1`).
		WithArgs(4, adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	_, err = svc.VerifyAdminPin(context.Background(), adminID, request.VerifyAdminPinRequestDTO{Pin: "000000"})
	var invErr *auth.PinInvalidError
	if !errors.As(err, &invErr) || invErr.RemainingAttempts != 1 {
		t.Fatalf("expected PinInvalidError with RemainingAttempts=1, got %v", err)
	}

	// Attempt #5 (failed_count goes from 4 -> 5, triggers 15-minute lockout!)
	mock.ExpectQuery(`SELECT account_pii_pin_hash, COALESCE\(account_pii_pin_failed_count, 0\), account_pii_pin_locked_until FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_pii_pin_hash", "account_pii_pin_failed_count", "account_pii_pin_locked_until"}).
			AddRow(pinHash, 4, nil))

	mock.ExpectExec(`UPDATE accounts SET account_pii_pin_failed_count = \$1, account_pii_pin_locked_until = \$2`).
		WithArgs(5, sqlmock.AnyArg(), adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	_, err = svc.VerifyAdminPin(context.Background(), adminID, request.VerifyAdminPinRequestDTO{Pin: "000000"})
	var lockErr *auth.PinLockoutError
	if !errors.As(err, &lockErr) {
		t.Fatalf("expected PinLockoutError on 5th failed attempt, got %v", err)
	}

	// Attempt while locked out -> rejected immediately even with the right PIN
	futureLock := time.Now().UTC().Add(14 * time.Minute)
	mock.ExpectQuery(`SELECT account_pii_pin_hash, COALESCE\(account_pii_pin_failed_count, 0\), account_pii_pin_locked_until FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_pii_pin_hash", "account_pii_pin_failed_count", "account_pii_pin_locked_until"}).
			AddRow(pinHash, 5, futureLock))

	_, err = svc.VerifyAdminPin(context.Background(), adminID, request.VerifyAdminPinRequestDTO{Pin: "654321"})
	if !errors.As(err, &lockErr) {
		t.Fatalf("expected PinLockoutError while locked out, got %v", err)
	}

	// HTTP Handler returns 429 Too Many Requests when locked out
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := auth.NewHandler(svc)
	r.POST("/api/v1/admin/verify-pin", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, adminID)
		c.Set(middleware.ContextKeyUserRole, "admin")
		h.VerifyAdminPin(c)
	})

	mock.ExpectQuery(`SELECT account_pii_pin_hash, COALESCE\(account_pii_pin_failed_count, 0\), account_pii_pin_locked_until FROM accounts`).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows([]string{"account_pii_pin_hash", "account_pii_pin_failed_count", "account_pii_pin_locked_until"}).
			AddRow(pinHash, 5, futureLock))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/verify-pin", strings.NewReader(`{"pin":"654321"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP 429 TooManyRequests on locked PIN, got %d (%s)", rec.Code, rec.Body.String())
	}
}
