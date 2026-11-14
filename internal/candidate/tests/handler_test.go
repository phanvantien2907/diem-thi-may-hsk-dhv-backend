package tests

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	svc := candidate.NewService(db, nil, nil, nil) // passing nil for audit, media, and vision services
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

func TestHandler_GetMyDocuments_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	h := candidate.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()

	r.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", int64(1))
	})
	r.GET("/me/candidate-profile/documents", h.GetMyDocuments)

	// Mock candidate lookup
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))

	// Mock document query returning 1 row
	docCols := []string{
		"candidate_document_id", "candidate_document_candidate_id",
		"candidate_document_type", "candidate_document_number",
		"candidate_document_issue_date", "candidate_document_issue_place",
		"candidate_document_front_image_url", "candidate_document_back_image_url",
		"candidate_document_portrait_image_url",
		"candidate_document_verification_status", "candidate_document_rejection_reason",
		"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
	}
	mock.ExpectQuery(`SELECT .+ FROM candidate_documents`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows(docCols).
			AddRow(1, 10, "cccd", "001099000001", time.Now(), "Police", "http://img/front.jpg", nil, "http://img/portrait.jpg", "pending", nil, nil, nil, nil))

	w := httptest.NewRecorder()
	req := testutils.BuildJSONRequest(http.MethodGet, "/me/candidate-profile/documents", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestHandler_GetMyDocuments_NoProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	h := candidate.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()

	r.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", int64(99))
	})
	r.GET("/me/candidate-profile/documents", h.GetMyDocuments)

	// Mock candidate lookup returning no rows → triggers "chưa có hồ sơ thí sinh"
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}))

	w := httptest.NewRecorder()
	req := testutils.BuildJSONRequest(http.MethodGet, "/me/candidate-profile/documents", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestHandler_ValidatePortrait_Validation(t *testing.T) {
	svc, _, db := setupCandidateMockService(t)
	defer db.Close()

	h := candidate.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()

	r.Use(func(ctx *gin.Context) {
		ctx.Set("user_id", int64(1))
	})
	r.POST("/me/candidate-profile/validate-portrait", h.ValidatePortrait)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty body is rejected (required)",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid url format",
			body:       `{"portrait_image_url":"not-a-url"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid url passes validation",
			body:       `{"portrait_image_url":"https://example.com/portrait.jpg"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := testutils.BuildJSONRequest(http.MethodPost, "/me/candidate-profile/validate-portrait", tt.body)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d; body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

