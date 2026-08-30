package tests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hskvinh/backend/internal/audit"
	"github.com/hskvinh/backend/internal/audit/dto/request"
	"github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/utils/testutils"
)

type fakeAuditService struct {
	listRes response.AuditLogListResponseDTO
	listErr error
}

func (f *fakeAuditService) Log(ctx context.Context, tx *sql.Tx, req request.CreateAuditLogRequestDTO) error {
	return nil
}

func (f *fakeAuditService) List(ctx context.Context, req request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error) {
	return f.listRes, f.listErr
}

func TestHandler_List(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockRes    response.AuditLogListResponseDTO
		mockErr    error
		wantStatus int
	}{
		{
			name:       "success",
			query:      "?page=1&limit=10",
			mockRes:    response.AuditLogListResponseDTO{},
			mockErr:    nil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid query",
			query:      "?page=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service error",
			query:      "?page=1",
			mockErr:    errors.New("db error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &fakeAuditService{
				listRes: tt.mockRes,
				listErr: tt.mockErr,
			}
			h := audit.NewHandler(mockSvc)
			_, _, r := testutils.SetupTestRouter()

			r.GET("/admin/audit-logs", h.List)

			w := httptest.NewRecorder()
			req := testutils.BuildJSONRequest(http.MethodGet, "/admin/audit-logs"+tt.query, nil)

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}
