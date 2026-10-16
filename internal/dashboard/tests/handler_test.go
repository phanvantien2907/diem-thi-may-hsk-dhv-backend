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
	"github.com/hskvinh/backend/internal/dashboard"
	"github.com/hskvinh/backend/internal/middleware"
)

type fakeAudit struct{ logErr error }

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return f.logErr
}
func (f *fakeAudit) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func setupHandlerTest(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, *dashboard.Handler) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	fa := &fakeAudit{}
	svc := dashboard.NewService(db, fa, nil)
	handler := dashboard.NewHandler(svc)

	r := gin.New()
	dashboard.RegisterRoutes(r.Group("/api/v1"), handler, "dummy_secret")

	return r, mock, handler
}

func TestHandler_ManualApprove_Validation(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "invalid id",
			id:         "abc",
			body:       `{"reason":"test reason","evidence_urls":["https://res.cloudinary.com/test.png"]}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Tham số id không hợp lệ",
		},
		{
			name:       "missing reason",
			id:         "10",
			body:       `{"evidence_urls":["https://res.cloudinary.com/test.png"]}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Reason",
		},
		{
			name:       "missing evidence_urls",
			id:         "10",
			body:       `{"reason":"test reason"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "EvidenceURLs",
		},
		{
			name:       "reason too short",
			id:         "10",
			body:       `{"reason":"123","evidence_urls":["https://res.cloudinary.com/test.png"]}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Reason",
		},
		{
			name:       "invalid JSON",
			id:         "10",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Dữ liệu yêu cầu không hợp lệ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/"+tt.id+"/manual-approve", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = []gin.Param{{Key: "id", Value: tt.id}}

			// Mock token claims
			c.Set(middleware.ContextKeyUserID, int64(1))

			h := dashboard.NewHandler(nil)
			h.ManualApprove(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
			
			var resp map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			
			if errResp, ok := resp["error"].(string); ok {
				if tt.wantMsg != "" && !strings.Contains(errResp, tt.wantMsg) {
					t.Errorf("error %q does not contain %q", errResp, tt.wantMsg)
				}
			} else if msg, ok := resp["msg"].(string); ok {
				if tt.wantMsg != "" && !strings.Contains(msg, tt.wantMsg) {
					t.Errorf("msg %q does not contain %q", msg, tt.wantMsg)
				}
			}
		})
	}
}

func TestHandler_GetRevenueChart_Validation(t *testing.T) {
	tests := []struct {
		name       string
		rangeParam string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "missing range",
			rangeParam: "",
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Tham số range là bắt buộc",
		},
		{
			name:       "invalid range",
			rangeParam: "15days",
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Tham số range không hợp lệ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			url := "/revenue-chart"
			if tt.rangeParam != "" {
				url += "?range=" + tt.rangeParam
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			c.Request = req

			h := dashboard.NewHandler(nil)
			h.GetRevenueChart(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
