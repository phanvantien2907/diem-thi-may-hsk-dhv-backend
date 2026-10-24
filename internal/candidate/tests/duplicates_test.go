package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/lib/pq"
)

func TestService_AdminListDuplicates_WithDuplicates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)

	mock.ExpectQuery(`(?s)SELECT candidate_document_type,\s+candidate_document_number,\s+ARRAY_AGG\(DISTINCT candidate_document_candidate_id.*FROM candidate_documents.*WHERE candidate_document_is_deleted = FALSE.*GROUP BY candidate_document_type, candidate_document_number.*HAVING COUNT\(DISTINCT candidate_document_candidate_id\) > 1`).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_type", "candidate_document_number", "candidate_ids", "cnt",
		}).AddRow("cccd", "038200001234", "{10,25}", 2).
			AddRow("passport", "B1234567", "{5,12,99}", 3))

	duplicates, err := svc.AdminListDuplicates(context.Background())
	if err != nil {
		t.Fatalf("AdminListDuplicates() error = %v", err)
	}

	if len(duplicates) != 2 {
		t.Fatalf("expected 2 duplicate groups, got %d", len(duplicates))
	}

	if duplicates[0].DocType != "cccd" || duplicates[0].DocNumber != "038200001234" || duplicates[0].Count != 2 {
		t.Errorf("unexpected first duplicate: %+v", duplicates[0])
	}
	if len(duplicates[0].CandidateIDs) != 2 || duplicates[0].CandidateIDs[0] != 10 || duplicates[0].CandidateIDs[1] != 25 {
		t.Errorf("unexpected candidate IDs for first item: %v", duplicates[0].CandidateIDs)
	}

	if duplicates[1].DocType != "passport" || duplicates[1].DocNumber != "B1234567" || duplicates[1].Count != 3 {
		t.Errorf("unexpected second duplicate: %+v", duplicates[1])
	}
	if len(duplicates[1].CandidateIDs) != 3 || duplicates[1].CandidateIDs[0] != 5 || duplicates[1].CandidateIDs[2] != 99 {
		t.Errorf("unexpected candidate IDs for second item: %v", duplicates[1].CandidateIDs)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminListDuplicates_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)

	mock.ExpectQuery(`(?s)SELECT candidate_document_type,\s+candidate_document_number.*FROM candidate_documents`).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_type", "candidate_document_number", "candidate_ids", "cnt",
		}))

	duplicates, err := svc.AdminListDuplicates(context.Background())
	if err != nil {
		t.Fatalf("AdminListDuplicates() error = %v", err)
	}

	if duplicates == nil || len(duplicates) != 0 {
		t.Fatalf("expected empty non-nil slice, got: %+v", duplicates)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandler_AdminListDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	h := candidate.NewHandler(svc)

	mock.ExpectQuery(`(?s)SELECT candidate_document_type,\s+candidate_document_number.*FROM candidate_documents`).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_type", "candidate_document_number", "candidate_ids", "cnt",
		}).AddRow("cccd", "038200001234", "{10,25}", 2))

	r := gin.New()
	r.GET("/admin/candidates/duplicates", h.AdminListDuplicates)

	req := httptest.NewRequest(http.MethodGet, "/admin/candidates/duplicates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool                               `json:"success"`
		Data    []response.DuplicateDocResponseDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success || len(resp.Data) != 1 || resp.Data[0].DocNumber != "038200001234" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_UploadDocument_DuplicateDocNumber(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)

	accountID := int64(1)
	candidateID := int64(100)

	// Fetch candidate ID
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(accountID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(candidateID))

	// Check candidate_documents for duplicate doc number -> finds existing record with candidate ID 200
	mock.ExpectQuery(`SELECT candidate_document_candidate_id FROM candidate_documents`).
		WithArgs("038200001234", candidateID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_document_candidate_id"}).AddRow(int64(200)))

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "038200001234",
		IssueDate:        "2020-01-01",
		IssuePlace:       "Cục CSQLHC về TTXH",
		FrontImageURL:    "https://example.com/front.jpg",
		PortraitImageURL: "https://example.com/portrait.jpg",
	}

	_, err = svc.UploadDocument(context.Background(), accountID, req)
	if err != candidate.ErrDuplicateDocumentNumber {
		t.Fatalf("expected ErrDuplicateDocumentNumber, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_UploadDocument_DuplicateAccountCCCD(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)

	accountID := int64(1)
	candidateID := int64(100)

	// Fetch candidate ID
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(accountID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(candidateID))

	// Check candidate_documents -> No rows
	mock.ExpectQuery(`SELECT candidate_document_candidate_id FROM candidate_documents`).
		WithArgs("038200001234", candidateID).
		WillReturnError(sql.ErrNoRows)

	// Check accounts -> finds conflicting account ID
	mock.ExpectQuery(`SELECT a\.account_id FROM accounts a`).
		WithArgs("038200001234", candidateID).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(999)))

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "038200001234",
		IssueDate:        "2020-01-01",
		IssuePlace:       "Cục CSQLHC về TTXH",
		FrontImageURL:    "https://example.com/front.jpg",
		PortraitImageURL: "https://example.com/portrait.jpg",
	}

	_, err = svc.UploadDocument(context.Background(), accountID, req)
	if err != candidate.ErrDuplicateDocumentNumber {
		t.Fatalf("expected ErrDuplicateDocumentNumber, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_CreateProfile_DuplicateProfileConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	accountID := int64(1)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO candidates`).
		WillReturnError(&pq.Error{
			Code:       "23505",
			Constraint: "uq_candidates_account",
			Message:    `duplicate key value violates unique constraint "uq_candidates_account"`,
		})
	mock.ExpectRollback()

	req := request.CreateProfileRequestDTO{
		FullName:    "Nguyen Van A",
		DOB:         "2000-01-01",
		Gender:      "male",
		Nationality: "Việt Nam",
	}

	_, err = svc.CreateProfile(context.Background(), req, accountID)
	if err != candidate.ErrCandidateProfileAlreadyExists {
		t.Fatalf("expected ErrCandidateProfileAlreadyExists, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
