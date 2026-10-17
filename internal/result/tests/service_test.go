package tests

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	auditdto "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/result"
	"github.com/stretchr/testify/assert"
)

type fakeAudit struct{}

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditdto.CreateAuditLogRequestDTO) error {
	return nil
}
func (f *fakeAudit) List(_ context.Context, _ auditdto.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func setupResultServiceTest(t *testing.T) (*result.Service, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	svc := result.NewService(db, &fakeAudit{})
	return svc, mock
}

func TestResultService_GetResultByRegistration(t *testing.T) {
	svc, mock := setupResultServiceTest(t)

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"exam_result_id", "exam_result_exam_registration_id",
		"exam_result_listening_score", "exam_result_reading_score",
		"exam_result_writing_score", "exam_result_total_score",
		"exam_result_status", "exam_result_published_at",
		"exam_result_is_deleted", "exam_result_deleted_at",
		"exam_result_created_at", "exam_result_updated_at",
	}).AddRow(1, 100, 90, 85, 95, 270, "passed", now, false, nil, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT exam_result_id, exam_result_exam_registration_id`)).
		WithArgs(int64(100)).
		WillReturnRows(rows)

	res, err := svc.GetResultByRegistration(context.Background(), 100)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.ID)
	assert.Equal(t, int64(100), res.RegistrationID)
	assert.Equal(t, float64(270), *res.Total)
	assert.Equal(t, "passed", *res.Status)
}

func TestResultService_ClearCache(t *testing.T) {
	svc, _ := setupResultServiceTest(t)

	// ClearCache with nil cache should not fail
	err := svc.ClearCache(context.Background())
	assert.NoError(t, err)
}

