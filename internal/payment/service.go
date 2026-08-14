package payment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/payment/dto/request"
	"github.com/hskvinh/backend/internal/payment/dto/response"
	payenum "github.com/hskvinh/backend/internal/payment/enum"
	"github.com/hskvinh/backend/internal/registration"
	regenum "github.com/hskvinh/backend/internal/registration/enum"
)

// Service handles payment creation and confirmation.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	regSvc   *registration.Service
}

// NewService creates a payment Service.
func NewService(db *sql.DB, auditSvc audit.Service, regSvc *registration.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc, regSvc: regSvc}
}

// CreatePayment inserts a pending payment and returns its public DTO.
func (s *Service) CreatePayment(ctx context.Context, req request.CreatePaymentRequestDTO) (response.PaymentResponseDTO, error) {
	if !payenum.PaymentMethod(req.Method).Valid() {
		return response.PaymentResponseDTO{}, fmt.Errorf("invalid payment method: %s", req.Method)
	}

	var regStatus regenum.ExamRegistrationStatus
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_registration_status FROM exam_registrations
		 WHERE exam_registration_id=$1 AND exam_registration_is_deleted=FALSE`,
		req.ExamRegistrationID,
	).Scan(&regStatus); errors.Is(err, sql.ErrNoRows) {
		return response.PaymentResponseDTO{}, errors.New("registration not found")
	} else if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: check reg: %w", err)
	}
	if regStatus == regenum.RegStatusCancelled {
		return response.PaymentResponseDTO{}, errors.New("cannot pay for a cancelled registration")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e PaymentEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO payments
		       (payment_exam_registration_id, payment_amount, payment_method, payment_status)
		VALUES ($1,$2,$3,'pending')
		RETURNING payment_id, payment_exam_registration_id, payment_amount, payment_method,
		          payment_transaction_ref, payment_status, payment_paid_at,
		          payment_is_deleted, payment_deleted_at, payment_created_at, payment_updated_at`,
		req.ExamRegistrationID, req.Amount, req.Method,
	).Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentTransactionRef, &e.PaymentStatus, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
	if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: insert: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"registration_id": req.ExamRegistrationID, "amount": req.Amount, "method": req.Method})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		Action:      auditenum.AuditActionPayment.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    e.PaymentID,
		NewValue:    newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: commit: %w", err)
	}
	return ToPaymentResponseDTO(e), nil
}

// ProcessCallback handles a gateway callback.
func (s *Service) ProcessCallback(ctx context.Context, req request.PaymentCallbackRequestDTO) (response.PaymentResponseDTO, error) {
	if !payenum.PaymentStatus(req.Status).Valid() {
		return response.PaymentResponseDTO{}, fmt.Errorf("invalid payment status: %s", req.Status)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var pay PaymentEntity
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_id, payment_exam_registration_id, payment_status
		FROM payments
		WHERE payment_transaction_ref=$1 AND payment_is_deleted=FALSE
		FOR UPDATE`, req.TransactionRef,
	).Scan(&pay.PaymentID, &pay.PaymentExamRegistrationID, &pay.PaymentStatus); errors.Is(err, sql.ErrNoRows) {
		return response.PaymentResponseDTO{}, errors.New("payment not found for this transaction reference")
	} else if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: lock: %w", err)
	}
	if pay.PaymentStatus != payenum.PayStatusPending {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment already processed: %s", pay.PaymentStatus)
	}

	var updated PaymentEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE payments
		SET payment_status    = $1,
		    payment_paid_at   = CASE WHEN $1='success' THEN NOW() ELSE NULL END,
		    payment_updated_at = NOW()
		WHERE payment_id=$2
		RETURNING payment_id, payment_exam_registration_id, payment_amount, payment_method,
		          payment_transaction_ref, payment_status, payment_paid_at,
		          payment_is_deleted, payment_deleted_at, payment_created_at, payment_updated_at`,
		req.Status, pay.PaymentID,
	).Scan(
		&updated.PaymentID, &updated.PaymentExamRegistrationID, &updated.PaymentAmount,
		&updated.PaymentMethod, &updated.PaymentTransactionRef, &updated.PaymentStatus,
		&updated.PaymentPaidAt, &updated.PaymentIsDeleted, &updated.PaymentDeletedAt,
		&updated.PaymentCreatedAt, &updated.PaymentUpdatedAt,
	); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: update: %w", err)
	}

	action := auditenum.AuditActionPayment
	if payenum.PaymentStatus(req.Status) == payenum.PayStatusRefunded {
		action = auditenum.AuditActionRefund
	}
	newVal, _ := json.Marshal(map[string]any{"status": req.Status, "transaction_ref": req.TransactionRef})
	oldVal, _ := json.Marshal(map[string]any{"status": "pending"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		Action:      action.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    pay.PaymentID,
		OldValue:    oldVal,
		NewValue:    newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: commit: %w", err)
	}

	if payenum.PaymentStatus(req.Status) == payenum.PayStatusSuccess {
		if err := s.regSvc.ConfirmRegistration(ctx, pay.PaymentExamRegistrationID); err != nil {
			return ToPaymentResponseDTO(updated), fmt.Errorf("payment: confirm reg: %w", err)
		}
	}
	return ToPaymentResponseDTO(updated), nil
}

// GetPaymentHistory returns a candidate's payment DTOs.
func (s *Service) GetPaymentHistory(ctx context.Context, candidateID int64) ([]response.PaymentResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.payment_id, p.payment_exam_registration_id, p.payment_amount,
		       p.payment_method, p.payment_transaction_ref, p.payment_status,
		       p.payment_paid_at, p.payment_is_deleted, p.payment_deleted_at,
		       p.payment_created_at, p.payment_updated_at
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		WHERE er.exam_registration_candidate_id=$1 AND p.payment_is_deleted=FALSE
		ORDER BY p.payment_created_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("payment: history: %w", err)
	}
	defer rows.Close()
	var list []response.PaymentResponseDTO
	for rows.Next() {
		var e PaymentEntity
		if err := rows.Scan(
			&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount,
			&e.PaymentMethod, &e.PaymentTransactionRef, &e.PaymentStatus,
			&e.PaymentPaidAt, &e.PaymentIsDeleted, &e.PaymentDeletedAt,
			&e.PaymentCreatedAt, &e.PaymentUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("payment: scan: %w", err)
		}
		list = append(list, ToPaymentResponseDTO(e))
	}
	return list, rows.Err()
}
