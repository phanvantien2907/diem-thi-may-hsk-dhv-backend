package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hskvinh/backend/internal/registration"
)

// Service handles payment creation and confirmation.
type Service struct {
	db     *sql.DB
	regSvc *registration.Service
}

// NewService creates a payment Service.
func NewService(db *sql.DB, regSvc *registration.Service) *Service {
	return &Service{db: db, regSvc: regSvc}
}

// CreatePayment inserts a pending payment record.
func (s *Service) CreatePayment(ctx context.Context, in CreatePaymentInput) (*Payment, error) {
	var regStatus registration.ExamRegistrationStatus
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_registration_status FROM exam_registrations
		 WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE`,
		in.RegistrationID,
	).Scan(&regStatus); errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("registration not found")
	} else if err != nil {
		return nil, fmt.Errorf("payment: check reg: %w", err)
	}
	if regStatus == registration.RegStatusCancelled {
		return nil, errors.New("cannot pay for a cancelled registration")
	}

	var p Payment
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO payments
		       (payment_exam_registration_id, payment_amount, payment_method, payment_status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING payment_id, payment_exam_registration_id, payment_amount, payment_method,
		          payment_transaction_ref, payment_status, payment_paid_at,
		          payment_is_deleted, payment_created_at, payment_updated_at`,
		in.RegistrationID, in.Amount, in.Method,
	).Scan(
		&p.PaymentID, &p.PaymentExamRegistrationID, &p.PaymentAmount, &p.PaymentMethod,
		&p.PaymentTransactionRef, &p.PaymentStatus, &p.PaymentPaidAt,
		&p.PaymentIsDeleted, &p.PaymentCreatedAt, &p.PaymentUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("payment: insert: %w", err)
	}
	return &p, nil
}

// ProcessCallback handles a gateway callback, marking the payment and
// confirming the registration on success.
func (s *Service) ProcessCallback(ctx context.Context, transactionRef string, status PaymentStatus) (*Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("payment: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var pay Payment
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_id, payment_exam_registration_id, payment_status
		FROM payments
		WHERE payment_transaction_ref = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, transactionRef,
	).Scan(&pay.PaymentID, &pay.PaymentExamRegistrationID, &pay.PaymentStatus); errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("payment not found for this transaction reference")
	} else if err != nil {
		return nil, fmt.Errorf("payment: lock: %w", err)
	}
	if pay.PaymentStatus != PayStatusPending {
		return nil, fmt.Errorf("payment already processed: %s", pay.PaymentStatus)
	}

	var updated Payment
	if err := tx.QueryRowContext(ctx, `
		UPDATE payments
		SET payment_status   = $1,
		    payment_paid_at  = CASE WHEN $1 = 'success' THEN NOW() ELSE NULL END,
		    payment_updated_at = NOW()
		WHERE payment_id = $2
		RETURNING payment_id, payment_exam_registration_id, payment_amount, payment_method,
		          payment_transaction_ref, payment_status, payment_paid_at,
		          payment_is_deleted, payment_created_at, payment_updated_at`,
		status, pay.PaymentID,
	).Scan(
		&updated.PaymentID, &updated.PaymentExamRegistrationID, &updated.PaymentAmount,
		&updated.PaymentMethod, &updated.PaymentTransactionRef, &updated.PaymentStatus,
		&updated.PaymentPaidAt, &updated.PaymentIsDeleted,
		&updated.PaymentCreatedAt, &updated.PaymentUpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("payment: update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("payment: commit: %w", err)
	}

	// Confirm registration in its own transaction after payment committed.
	if status == PayStatusSuccess {
		if err := s.regSvc.ConfirmRegistration(ctx, pay.PaymentExamRegistrationID); err != nil {
			return &updated, fmt.Errorf("payment: confirm reg: %w", err)
		}
	}
	return &updated, nil
}

// GetPaymentHistory returns all non-deleted payments for a candidate via JOIN.
func (s *Service) GetPaymentHistory(ctx context.Context, candidateID int64) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.payment_id, p.payment_exam_registration_id, p.payment_amount,
		       p.payment_method, p.payment_transaction_ref, p.payment_status,
		       p.payment_paid_at, p.payment_is_deleted,
		       p.payment_created_at, p.payment_updated_at
		FROM payments p
		JOIN exam_registrations er
		  ON er.exam_registration_id = p.payment_exam_registration_id
		WHERE er.exam_registration_candidate_id = $1 AND p.payment_is_deleted = FALSE
		ORDER BY p.payment_created_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("payment: history: %w", err)
	}
	defer rows.Close()
	var list []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(
			&p.PaymentID, &p.PaymentExamRegistrationID, &p.PaymentAmount,
			&p.PaymentMethod, &p.PaymentTransactionRef, &p.PaymentStatus,
			&p.PaymentPaidAt, &p.PaymentIsDeleted,
			&p.PaymentCreatedAt, &p.PaymentUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("payment: scan: %w", err)
		}
		list = append(list, p)
	}
	return list, rows.Err()
}
