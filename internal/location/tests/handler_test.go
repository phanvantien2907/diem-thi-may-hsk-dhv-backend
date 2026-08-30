package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/location"
	"github.com/hskvinh/backend/internal/location/dto/response"
)

func setupHandlerTest(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	fa := &fakeAudit{}
	svc := location.NewService(db, fa)
	handler := location.NewHandler(svc)

	r := gin.New()
	location.RegisterRoutes(r.Group("/api/v1"), handler, "dummy_secret")

	return r, mock
}

func TestHandler_ListProvinces(t *testing.T) {
	r, mock := setupHandlerTest(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT province_id, province_code, province_name FROM provinces WHERE province_is_deleted = FALSE ORDER BY province_name ASC`)).
		WillReturnRows(sqlmock.NewRows([]string{"province_id", "province_code", "province_name"}).
			AddRow(1, "01", "Hà Nội"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/provinces", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Success bool                           `json:"success"`
		Data    []response.ProvinceResponseDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	if len(resp.Data) != 1 || resp.Data[0].Name != "Hà Nội" {
		t.Errorf("unexpected response body: %s", w.Body.String())
	}
}

func TestHandler_GetWardDeliveryCoverage(t *testing.T) {
	r, mock := setupHandlerTest(t)

	// Test case without courier param
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type, ward_ghn_coverage, ward_ghtk_coverage FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"ward_id", "ward_province_id", "ward_code", "ward_name", "ward_type", "ward_ghn_coverage", "ward_ghtk_coverage"}).
			AddRow(10, 1, "001", "Phường 1", "Phường", true, false))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/wards/10/delivery-coverage", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Success bool                             `json:"success"`
		Data    response.WardCoverageResponseDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	if !resp.Data.Coverage["ghn"] || resp.Data.Coverage["ghtk"] {
		t.Errorf("unexpected coverage values: %v", resp.Data.Coverage)
	}
}

func TestHandler_MergeWard_Validation(t *testing.T) {
	// Without hitting DB, the request validation should fail because Reason is missing or TargetWardID is 0
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "missing target ID",
			body:       `{"reason": "Sáp nhập"}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "TargetWardID",
		},
		{
			name:       "missing reason",
			body:       `{"target_ward_id": 20}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Reason",
		},
		{
			name:       "invalid JSON",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			// Mocking middleware by directly passing token is complex without full JWT setup,
			// but gin parses binding BEFORE auth? No, auth is first.
			// Wait, we need a valid JWT token to pass the middleware, or we can just bypass it
			// for handler tests by calling handler.MergeWard directly.
			// Let's call the handler method directly to skip the middleware layer!

			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/merge", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = []gin.Param{{Key: "id", Value: "10"}}
			
			// Mock token claims to bypass utils.MustGetAccountID panics
			c.Set("account_id", int64(1))

			// Create a dummy handler struct since we need access to the method
			h := location.NewHandler(nil) // service is nil, but validation happens before service call
			h.MergeWard(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
