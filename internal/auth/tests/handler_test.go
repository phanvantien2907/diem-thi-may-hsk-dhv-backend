package tests

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/auth"
	"github.com/hskvinh/backend/internal/utils/testutils"
)

func setupAuthMockService(t *testing.T) (*auth.Service, sqlmock.Sqlmock, *sql.DB) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	svc := auth.NewService(db, nil, "secret", 15*time.Minute, 24*time.Hour)
	return svc, mock, db
}

func TestHandler_Login_Validation(t *testing.T) {
	svc, _, db := setupAuthMockService(t)
	defer db.Close()

	h := auth.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()
	r.POST("/auth/login", h.Login)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty body",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing email",
			body:       `{"password":"password123"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid email",
			body:       `{"email":"invalid-email","password":"password123"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := testutils.BuildJSONRequest(http.MethodPost, "/auth/login", tt.body)

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}
