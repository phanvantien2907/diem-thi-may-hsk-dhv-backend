package registration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles seat reservation and registration lifecycle.
type Service struct{ db *sql.DB }

// NewService creates a registration Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ReserveSeat atomically holds an available seat and creates an exam_registration.
// Uses SELECT FOR UPDATE SKIP LOCKED so many goroutines can work concurrently
// without blocking each other on the same session.
func (s *Service) ReserveSeat(ctx context.Context, sessionID, candidateID int64) (*ExamRegistration, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Guard: no duplicate active registration.
	var dup bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM exam_registrations
			WHERE exam_registration_candidate_id    = $1
			  AND exam_registration_exam_session_id = $2
			  AND exam_registration_status         != 'cancelled'
			  AND exam_registration_is_deleted      = FALSE
		)`, candidateID, sessionID,
	).Scan(&dup); err != nil {
		return nil, fmt.Errorf("registration: check dup: %w", err)
	}
	if dup {
		return nil, errors.New("already registered for this exam session")
	}

	// Lock one available seat (SKIP LOCKED = true concurrency, no queuing).
	var seatID int64
	err = tx.QueryRowContext(ctx, `
		SELECT exam_seat_id
		FROM exam_seats
		WHERE exam_seat_exam_session_id = $1
		  AND exam_seat_status          = 'available'
		  AND exam_seat_is_deleted      = FALSE
		ORDER BY exam_seat_number ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, sessionID,
	).Scan(&seatID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no available seats for this exam session")
	}
	if err != nil {
		return nil, fmt.Errorf("registration: lock seat: %w", err)
	}

	// Hold seat for 15 minutes.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status     = 'held',
		    exam_seat_held_until  = NOW() + INTERVAL '15 minutes',
		    exam_seat_updated_at  = NOW()
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE`, seatID,
	); err != nil {
		return nil, fmt.Errorf("registration: hold seat: %w", err)
	}

	// Create registration.
	var reg ExamRegistration
	err = tx.QueryRowContext(ctx, `
		INSERT INTO exam_registrations
		       (exam_registration_candidate_id, exam_registration_exam_session_id,
		        exam_registration_exam_seat_id, exam_registration_status)
		VALUES ($1, $2, $3, 'pending_payment')
		RETURNING exam_registration_id, exam_registration_candidate_id,
		          exam_registration_exam_session_id, exam_registration_exam_seat_id,
		          exam_registration_status, exam_registration_registered_at,
		          exam_registration_is_deleted, exam_registration_created_at,
		          exam_registration_updated_at`,
		candidateID, sessionID, seatID,
	).Scan(
		&reg.ExamRegistrationID, &reg.ExamRegistrationCandidateID,
		&reg.ExamRegistrationExamSessionID, &reg.ExamRegistrationExamSeatID,
		&reg.ExamRegistrationStatus, &reg.ExamRegistrationRegisteredAt,
		&reg.ExamRegistrationIsDeleted, &reg.ExamRegistrationCreatedAt,
		&reg.ExamRegistrationUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("registration: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("registration: commit: %w", err)
	}
	return &reg, nil
}

// ConfirmRegistration transitions to 'confirmed' and marks seat as 'booked'.
func (s *Service) ConfirmRegistration(ctx context.Context, registrationID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID int64
	var status ExamRegistrationStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status); errors.Is(err, sql.ErrNoRows) {
		return errors.New("registration not found")
	} else if err != nil {
		return fmt.Errorf("registration: lock reg: %w", err)
	}
	if status == RegStatusCancelled {
		return errors.New("cannot confirm a cancelled registration")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations
		SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: confirm: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: book seat: %w", err)
	}
	return tx.Commit()
}

// CancelRegistration cancels a registration and frees the seat.
func (s *Service) CancelRegistration(ctx context.Context, registrationID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID int64
	var status ExamRegistrationStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status); errors.Is(err, sql.ErrNoRows) {
		return errors.New("registration not found")
	} else if err != nil {
		return fmt.Errorf("registration: lock reg: %w", err)
	}
	if status == RegStatusCancelled {
		return errors.New("registration is already cancelled")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations
		SET exam_registration_status = 'cancelled', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: cancel: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status = 'available', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: release seat: %w", err)
	}
	return tx.Commit()
}

// GetMyRegistrations returns all non-deleted registrations for a candidate.
func (s *Service) GetMyRegistrations(ctx context.Context, candidateID int64) ([]ExamRegistration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at,
		       exam_registration_is_deleted, exam_registration_created_at,
		       exam_registration_updated_at
		FROM exam_registrations
		WHERE exam_registration_candidate_id = $1 AND exam_registration_is_deleted = FALSE
		ORDER BY exam_registration_registered_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("registration: list: %w", err)
	}
	defer rows.Close()
	var list []ExamRegistration
	for rows.Next() {
		var r ExamRegistration
		if err := rows.Scan(
			&r.ExamRegistrationID, &r.ExamRegistrationCandidateID,
			&r.ExamRegistrationExamSessionID, &r.ExamRegistrationExamSeatID,
			&r.ExamRegistrationStatus, &r.ExamRegistrationRegisteredAt,
			&r.ExamRegistrationIsDeleted, &r.ExamRegistrationCreatedAt,
			&r.ExamRegistrationUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("registration: scan: %w", err)
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// AutoReleaseHeldSeats resets expired held seats. Intended for a cron goroutine.
func (s *Service) AutoReleaseHeldSeats(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status     = 'available',
		    exam_seat_held_until  = NULL,
		    exam_seat_updated_at  = NOW()
		WHERE exam_seat_status    = 'held'
		  AND exam_seat_held_until < NOW()
		  AND exam_seat_is_deleted = FALSE`)
	if err != nil {
		return 0, fmt.Errorf("registration: auto release: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
