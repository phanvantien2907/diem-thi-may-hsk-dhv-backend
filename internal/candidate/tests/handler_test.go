package tests

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/utils/testutils"
)

func setupCandidateMockService(t *testing.T) (*candidate.Service, sqlmock.Sqlmock, *sql.DB) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	svc := candidate.NewService(db, nil, nil) // passing nil for audit and media services
	return svc, mock, db
}

func TestHandler_PatchMyProfile_Validation(t *testing.T) {
	svc, _, db := setupCandidateMockService(t)
	defer db.Close()

	h := candidate.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()

	// Mock middleware setting user_id
	r.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", int64(1))
	})
	r.PATCH("/me/candidate-profile", h.PatchMyProfile)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty body is accepted for partial patch (struct valid)",
			body:       `{}`,
			wantStatus: http.StatusNotFound, // validation passes, DB says not found (mocked)
		},
		{
			name:       "invalid negative study years",
			body:       `{"chinese_study_years":-1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid study years",
			body:       `{"chinese_study_years":2}`,
			wantStatus: http.StatusNotFound, // passes validation, fails in mocked DB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := testutils.BuildJSONRequest(http.MethodPatch, "/me/candidate-profile", tt.body)

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}
