package registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/registration/dto/response"
	"github.com/hskvinh/backend/internal/registration/entities"
	regenum "github.com/hskvinh/backend/internal/registration/enum"
)

// Service handles seat reservation and registration lifecycle.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
}

// NewService creates a registration Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

// ReserveSeat atomically holds an available seat and creates an exam_registration using DB stored procedure.
func (s *Service) ReserveSeat(ctx context.Context, req request.ReserveSeatRequestDTO) (response.ExamRegistrationResponseDTO, error) {
	var regID int64
	err := s.db.QueryRowContext(ctx, "SELECT fn_book_exam_seat($1, $2, 15)", req.CandidateID, req.ExamSessionID).Scan(&regID)
	if err != nil {
		// Check for specific Postgres exceptions thrown by fn_book_exam_seat
		if err.Error() == "pq: DUPLICATE_REGISTRATION" {
			return response.ExamRegistrationResponseDTO{}, errors.New("Bạn đã đăng ký ca thi này rồi")
		}
		if err.Error() == "pq: NO_SEAT_AVAILABLE" {
			return response.ExamRegistrationResponseDTO{}, errors.New("Đã hết ghế trống cho ca thi này")
		}
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: book seat: %w", err)
	}

	// Fetch the newly created registration to return
	var e entities.ExamRegistrationEntity
	err = s.db.QueryRowContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_id = $1`, regID,
	).Scan(
		&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
		&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
		&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
	)
	if err != nil {
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: fetch after insert: %w", err)
	}

	// Async audit logging (fire-and-forget for performance, or standard call if needed)
	newVal, _ := json.Marshal(map[string]any{"candidate_id": req.CandidateID, "session_id": req.ExamSessionID, "seat_id": e.ExamRegistrationExamSeatID})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &req.CandidateID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       e.ExamRegistrationID,
		NewValue:       newVal,
	})

	return ToRegistrationResponseDTO(e), nil
}

// ConfirmRegistration transitions to 'confirmed' and marks seat as 'booked'.
// Note: In a real flow with payments, fn_confirm_registration_payment should be called by the payment module instead.
func (s *Service) ConfirmRegistration(ctx context.Context, registrationID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID, candidateID int64
	var status regenum.ExamRegistrationStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status, exam_registration_candidate_id
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status, &candidateID); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return fmt.Errorf("registration: lock: %w", err)
	}
	if status == regenum.RegStatusCancelled {
		return errors.New("Không thể xác nhận phiếu đăng ký đã hủy")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_registrations SET exam_registration_status='confirmed', exam_registration_updated_at=NOW()
		 WHERE exam_registration_id=$1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: confirm: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_seats SET exam_seat_status='booked', exam_seat_held_until=NULL, exam_seat_updated_at=NOW()
		 WHERE exam_seat_id=$1 AND exam_seat_is_deleted=FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: book seat: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"status": "confirmed"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// CancelRegistration cancels a registration and frees the seat.
func (s *Service) CancelRegistration(ctx context.Context, registrationID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID, candidateID int64
	var status regenum.ExamRegistrationStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status, exam_registration_candidate_id
		FROM exam_registrations
		WHERE exam_registration_id=$1 AND exam_registration_is_deleted=FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status, &candidateID); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return fmt.Errorf("registration: lock: %w", err)
	}
	if status == regenum.RegStatusCancelled {
		return errors.New("Phiếu đăng ký đã bị hủy từ trước")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_registrations SET exam_registration_status='cancelled', exam_registration_updated_at=NOW()
		 WHERE exam_registration_id=$1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: cancel: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_seats SET exam_seat_status='available', exam_seat_held_until=NULL, exam_seat_updated_at=NOW()
		 WHERE exam_seat_id=$1 AND exam_seat_is_deleted=FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: release seat: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": string(status)})
	newVal, _ := json.Marshal(map[string]any{"status": "cancelled"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionDelete.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// GetMyRegistrations returns a candidate's registration DTOs (using lean projection).
func (s *Service) GetMyRegistrations(ctx context.Context, candidateID int64) ([]response.ExamRegistrationResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_candidate_id=$1 AND exam_registration_is_deleted=FALSE
		ORDER BY exam_registration_registered_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("registration: list: %w", err)
	}
	defer rows.Close()

	var list []response.ExamRegistrationResponseDTO
	for rows.Next() {
		var e entities.ExamRegistrationEntity
		if err := rows.Scan(
			&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
			&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
			&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
		); err != nil {
			return nil, fmt.Errorf("registration: scan: %w", err)
		}
		list = append(list, ToRegistrationResponseDTO(e))
	}
	return list, rows.Err()
}

// AutoReleaseHeldSeats resets expired held seats using the optimized stored procedure.
func (s *Service) AutoReleaseHeldSeats(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT fn_release_expired_seat_holds()").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("registration: auto release procedure: %w", err)
	}
	return count, nil
}
