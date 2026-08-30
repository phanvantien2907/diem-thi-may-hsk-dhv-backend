package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"database/sql"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditres "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/enum"
)

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
	svc := candidate.NewService(db, &fakeAuditService{}, fakeMedia)

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
	svc := candidate.NewService(db, &fakeAuditService{}, fakeMedia)

	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(10)))

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO candidate_documents`).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_document_id", "candidate_document_candidate_id", "candidate_document_type", "candidate_document_number", "candidate_document_issue_date", "candidate_document_issue_place", "candidate_document_front_image_url", "candidate_document_back_image_url", "candidate_document_portrait_image_url", "candidate_document_verification_status", "candidate_document_rejection_reason", "candidate_document_ward_id", "candidate_document_address_detail"}).
			AddRow(1, 10, "cccd", "123456789", time.Now(), "Police", "http://fake.com/1", "http://fake.com/2", "http://fake.com/3", "pending", nil, nil, nil))
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
