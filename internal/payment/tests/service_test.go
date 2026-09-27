package tests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/payment"
)

type fakeAudit struct{ logErr error }

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return f.logErr
}
func (f *fakeAudit) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func setupServiceTest(t *testing.T) (*payment.Service, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	fa := &fakeAudit{}
	svc := payment.NewService(db, fa, payment.PayOSConfig{
		ClientID:    "test-client",
		ApiKey:      "test-api",
		ChecksumKey: "test-checksum",
	})
	return svc, mock
}

func TestService_AdminCancelPayment_Success(t *testing.T) {
	svc, mock := setupServiceTest(t)
	ctx := context.Background()

	now := time.Now()

	// 1. Call fn_admin_cancel_payment
	mock.ExpectQuery(`^SELECT fn_admin_cancel_payment\(\$1\)$`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"fn_admin_cancel_payment"}).AddRow("failed"))

	// 2. Fetch updated payment row
	mock.ExpectQuery(`^SELECT payment_id,.* FROM payments WHERE payment_id = \$1 AND payment_is_deleted = FALSE$`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"payment_id", "payment_exam_registration_id", "payment_amount", "payment_method",
			"payment_gateway", "payment_transaction_ref", "payment_gateway_trans_id", "payment_content",
			"payment_status", "payment_expires_at", "payment_paid_at",
			"payment_is_deleted", "payment_deleted_at", "payment_created_at", "payment_updated_at",
		}).AddRow(
			10, 5, 500000, "payos",
			"payos", "10", "trans123", "HSK-5",
			"failed", nil, nil,
			false, nil, now, now,
		))

	resp, err := svc.AdminCancelPayment(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != 10 {
		t.Errorf("expected ID 10, got %d", resp.ID)
	}
	if resp.Status != "failed" {
		t.Errorf("expected status failed, got %s", resp.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestService_AdminConfirmPayment_Success(t *testing.T) {
	svc, mock := setupServiceTest(t)
	ctx := context.Background()
	now := time.Now()

	// 1. Call fn_admin_confirm_payment
	mock.ExpectQuery(`^SELECT fn_admin_confirm_payment\(\$1\)$`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"fn_admin_confirm_payment"}).AddRow(int64(10)))

	// 2. Fetch updated payment row
	mock.ExpectQuery(`^SELECT payment_id,.* FROM payments WHERE payment_id = \$1 AND payment_is_deleted = FALSE$`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"payment_id", "payment_exam_registration_id", "payment_amount", "payment_method",
			"payment_gateway", "payment_transaction_ref", "payment_gateway_trans_id", "payment_content",
			"payment_status", "payment_expires_at", "payment_paid_at",
			"payment_is_deleted", "payment_deleted_at", "payment_created_at", "payment_updated_at",
		}).AddRow(
			10, 5, 500000, "cash",
			"payos", "10", "trans123", "HSK-5",
			"success", nil, now,
			false, nil, now, now,
		))

	resp, err := svc.AdminConfirmPayment(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != 10 {
		t.Errorf("expected ID 10, got %d", resp.ID)
	}
	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
