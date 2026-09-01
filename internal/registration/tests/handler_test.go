package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/registration"
)

type fakeAudit struct{ logErr error }

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return f.logErr
}
func (f *fakeAudit) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func setupHandlerTest(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	fa := &fakeAudit{}
	svc := registration.NewService(db, fa)
	handler := registration.NewHandler(svc)

	r := gin.New()
	registration.RegisterRoutes(r.Group("/api/v1"), handler, "dummy_secret")

	return r, mock
}

func TestHandler_ReserveSeat_Validation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "missing session ID",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "ExamSessionID",
		},
		{
			name:       "invalid session ID",
			body:       `{"exam_session_id": -1}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "ExamSessionID",
		},
		{
			name:       "invalid JSON",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Dữ liệu yêu cầu không hợp lệ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/reserve", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req

			// Mock token claims
			c.Set("account_id", int64(1))

			h := registration.NewHandler(nil)
			h.ReserveSeat(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
			
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			
			// For validation errors, they usually go to the error field inside utils.BadRequest
			if errResp, ok := resp["error"].(string); ok {
				if tt.wantMsg != "" && !strings.Contains(errResp, tt.wantMsg) {
					t.Errorf("error %q does not contain %q", errResp, tt.wantMsg)
				}
			}
		})
	}
}

func TestHandler_PaymentConfirm_Validation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "missing amount",
			body:       `{"method":"vnpay","transaction_ref":"T123"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Amount",
		},
		{
			name:       "zero amount",
			body:       `{"amount":0,"method":"vnpay","transaction_ref":"T123"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Amount",
		},
		{
			name:       "missing method",
			body:       `{"amount":100000,"transaction_ref":"T123"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Method",
		},
		{
			name:       "missing transaction ref",
			body:       `{"amount":100000,"method":"vnpay"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "TransactionRef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/confirm", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = []gin.Param{{Key: "id", Value: "10"}}
			c.Set("account_id", int64(1))

			h := registration.NewHandler(nil)
			h.PaymentConfirm(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
			
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			
			if errResp, ok := resp["error"].(string); ok {
				if tt.wantMsg != "" && !strings.Contains(errResp, tt.wantMsg) {
					t.Errorf("error %q does not contain %q", errResp, tt.wantMsg)
				}
			}
		})
	}
}
