package tests

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditres "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/enum"
)

func TestService_PatchMyProfile_SavesChineseName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &fakeAuditService{}, &fakeMediaService{}, nil)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	dob := time.Date(2000, 5, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE candidates[\s\S]*candidate_full_name_cn[\s\S]*RETURNING`).
		WithArgs("", "潘文进", "", "", "", (*int16)(nil), int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_full_name_cn",
			"candidate_dob", "candidate_gender", "candidate_ethnicity", "candidate_religion",
			"candidate_birthplace", "candidate_nationality", "candidate_mother_tongue",
			"candidate_chinese_study_years",
		}).AddRow(10, 1, "Nguyễn Văn A", "潘文进", dob, "male", nil, nil, nil, "Việt Nam", nil, nil))
	mock.ExpectExec(`UPDATE candidate_documents`).WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	got, err := svc.PatchMyProfile(context.Background(), 1, request.PatchProfileRequestDTO{FullNameCN: "潘文进"})
	if err != nil {
		t.Fatalf("PatchMyProfile() error = %v", err)
	}
	if got.FullNameCN == nil || *got.FullNameCN != "潘文进" {
		t.Fatalf("FullNameCN = %v, want 潘文进", got.FullNameCN)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_PatchMyProfile_RemovesAccidentalJSONQuotesFromChineseName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &fakeAuditService{}, &fakeMediaService{}, nil)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	dob := time.Date(2000, 5, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE candidates[\s\S]*candidate_full_name_cn[\s\S]*RETURNING`).
		WithArgs("", "潘文进", "", "", "", (*int16)(nil), int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_full_name_cn",
			"candidate_dob", "candidate_gender", "candidate_ethnicity", "candidate_religion",
			"candidate_birthplace", "candidate_nationality", "candidate_mother_tongue",
			"candidate_chinese_study_years",
		}).AddRow(10, 1, "Nguyễn Văn A", "潘文进", dob, "male", nil, nil, nil, "Việt Nam", nil, nil))
	mock.ExpectExec(`UPDATE candidate_documents`).WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	got, err := svc.PatchMyProfile(context.Background(), 1, request.PatchProfileRequestDTO{
		FullNameCN: `"潘文进"`,
	})
	if err != nil {
		t.Fatalf("PatchMyProfile() error = %v", err)
	}
	if got.FullNameCN == nil || *got.FullNameCN != "潘文进" {
		t.Fatalf("FullNameCN = %v, want 潘文进", got.FullNameCN)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// fakeMediaService implements candidate.MediaService for testing.
type fakeMediaService struct {
	mu          sync.Mutex
	DeleteCount int
}

func (f *fakeMediaService) GenerateSignature() (string, int64, string, string, error) {
	return "fake_signature", 1234567890, "fake_key", "fake_cloud", nil
}

func (f *fakeMediaService) DeleteImage(ctx context.Context, publicID string) error {
	f.mu.Lock()
	f.DeleteCount++
	f.mu.Unlock()
	return nil
}

type fakeAuditService struct{}

func (f *fakeAuditService) Log(ctx context.Context, tx *sql.Tx, req auditreq.CreateAuditLogRequestDTO) error {
	return nil
}

func (f *fakeAuditService) List(ctx context.Context, req auditreq.ListAuditLogRequestDTO) (auditres.AuditLogListResponseDTO, error) {
	return auditres.AuditLogListResponseDTO{}, nil
}

func TestService_GetUploadSignature(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	fakeMedia := &fakeMediaService{}
	svc := candidate.NewService(db, &fakeAuditService{}, fakeMedia, nil)

	res, err := svc.GetUploadSignature(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Signature != "fake_signature" || res.APIKey != "fake_key" || res.CloudName != "fake_cloud" {
		t.Errorf("unexpected signature response: %+v", res)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestService_UploadDocument_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	fakeMedia := &fakeMediaService{}
	svc := candidate.NewService(db, &fakeAuditService{}, fakeMedia, nil)

	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))

	mock.ExpectQuery(`SELECT candidate_document_candidate_id FROM candidate_documents`).
		WithArgs("123456789", int64(10)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT a.account_id FROM accounts a`).
		WithArgs("123456789", int64(10)).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO candidate_documents`).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_document_id", "candidate_document_candidate_id", "candidate_document_type", "candidate_document_number", "candidate_document_issue_date", "candidate_document_issue_place", "candidate_document_front_image_url", "candidate_document_back_image_url", "candidate_document_portrait_image_url", "candidate_document_verification_status", "candidate_document_rejection_reason", "candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail"}).
			AddRow(1, 10, "cccd", "123456789", time.Now(), "Police", "http://fake.com/1", "http://fake.com/2", "http://fake.com/3", "pending", nil, nil, nil, nil))
	mock.ExpectCommit()

	req := request.UploadDocumentRequestDTO{
		DocType:          string(enum.DocumentTypeCCCD),
		DocNumber:        "123456789",
		IssueDate:        "2020-01-01",
		IssuePlace:       "Police",
		FrontImageURL:    "http://fake.com/1",
		BackImageURL:     "http://fake.com/2",
		PortraitImageURL: "http://fake.com/3",
	}

	_, err = svc.UploadDocument(context.Background(), int64(1), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestService_GetMyDocuments_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &fakeAuditService{}, &fakeMediaService{}, nil)

	// Mock candidate lookup
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))

	// Mock document query returning 2 rows
	docCols := []string{
		"candidate_document_id", "candidate_document_candidate_id",
		"candidate_document_type", "candidate_document_number",
		"candidate_document_issue_date", "candidate_document_issue_place",
		"candidate_document_front_image_url", "candidate_document_back_image_url",
		"candidate_document_portrait_image_url",
		"candidate_document_verification_status", "candidate_document_rejection_reason",
		"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
	}
	issueDate := time.Date(2021, 6, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .+ FROM candidate_documents`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows(docCols).
			AddRow(2, 10, "cccd", "012345678901", issueDate, "Police", "http://img/front2.jpg", nil, "http://img/portrait2.jpg", "verified", nil, nil, nil, nil).
			AddRow(1, 10, "cccd", "012345678901", issueDate, "Police", "http://img/front1.jpg", nil, "http://img/portrait1.jpg", "pending", nil, nil, nil, nil))

	docs, err := svc.GetMyDocuments(context.Background(), int64(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].CandidateDocumentID != 2 {
		t.Errorf("expected first doc ID 2, got %d", docs[0].CandidateDocumentID)
	}
	if string(docs[0].CandidateDocumentVerificationStatus) != "verified" {
		t.Errorf("expected first doc status 'verified', got %s", docs[0].CandidateDocumentVerificationStatus)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestService_GetMyDocuments_NoCandidateProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &fakeAuditService{}, &fakeMediaService{}, nil)

	// Mock candidate lookup returning no rows
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}))

	_, err = svc.GetMyDocuments(context.Background(), int64(99))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "chưa có hồ sơ thí sinh" {
		t.Errorf("expected Vietnamese not-found error, got: %s", err.Error())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}
