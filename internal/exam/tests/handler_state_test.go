package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/exam/enum"
	"github.com/hskvinh/backend/internal/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

// newExamContext builds a Gin context that looks like it came through JWTAuth with
// the :id path parameter already parsed.
func newExamContext(method, path, id string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set(middleware.ContextKeyUserID, int64(1))
	return c, rec
}

// decodeException reads the unified exception envelope.
func decodeException(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return payload
}

// TestHandlerIllegalTransitionReturns409WithCode pins the contract every endpoint
// relies on: a guard rejection is HTTP 409 with code INVALID_STATE_TRANSITION and a
// Vietnamese reason in msg.
func TestHandlerIllegalTransitionReturns409WithCode(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationOpen, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectRollback()

	h := exam.NewHandler(svc)
	c, rec := newExamContext(http.MethodPost, "/api/v1/admin/exam-periods/11/publish", "11", "")
	h.AdminPublishExamPeriod(c)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	payload := decodeException(t, rec)
	if payload["code"] != "INVALID_STATE_TRANSITION" {
		t.Fatalf("code = %v, want INVALID_STATE_TRANSITION", payload["code"])
	}
	if msg, _ := payload["msg"].(string); strings.TrimSpace(msg) == "" {
		t.Fatal("msg must explain the rejection in Vietnamese")
	}
	if status, _ := payload["statusCode"].(float64); int(status) != http.StatusConflict {
		t.Fatalf("statusCode = %v, want 409", payload["statusCode"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHandlerInvalidIDReturns400 covers the shared path-parameter guard.
func TestHandlerInvalidIDReturns400(t *testing.T) {
	svc, _ := newMockService(t)
	h := exam.NewHandler(svc)

	c, rec := newExamContext(http.MethodPost, "/api/v1/admin/exam-periods/abc/complete", "abc", "")
	h.AdminCompleteExamPeriod(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestHandlerValidationErrorReturns400 covers a service-level ValidationError.
func TestHandlerValidationErrorReturns400(t *testing.T) {
	svc, _ := newMockService(t)
	h := exam.NewHandler(svc)

	c, rec := newExamContext(http.MethodPost, "/api/v1/admin/exam-periods/11/reopen-registration", "11", "{}")
	h.AdminReopenPeriodRegistration(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if code, _ := decodeException(t, rec)["code"].(string); code != "" {
		t.Fatalf("code = %q, want empty for a plain 400", code)
	}
}

// TestHandlerNotFoundReturns404 covers the ErrExamPeriodNotFound mapping.
func TestHandlerNotFoundReturns404(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(999)).
		WillReturnRows(sqlmock.NewRows(periodRowColumns))
	mock.ExpectRollback()

	h := exam.NewHandler(svc)
	c, rec := newExamContext(http.MethodPost, "/api/v1/admin/exam-periods/999/complete", "999", "")
	h.AdminCompleteExamPeriod(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
