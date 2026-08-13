package exam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles exam types, rooms, sessions, and seat generation.
type Service struct{ db *sql.DB }

// NewService creates an exam Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListExamTypes returns all non-deleted exam types.
func (s *Service) ListExamTypes(ctx context.Context) ([]ExamType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_type_id, exam_type_code, exam_type_name, exam_type_description,
		       exam_type_is_deleted, exam_type_created_at, exam_type_updated_at
		FROM exam_types
		WHERE exam_type_is_deleted = FALSE
		ORDER BY exam_type_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list types: %w", err)
	}
	defer rows.Close()
	var list []ExamType
	for rows.Next() {
		var et ExamType
		if err := rows.Scan(
			&et.ExamTypeID, &et.ExamTypeCode, &et.ExamTypeName, &et.ExamTypeDescription,
			&et.ExamTypeIsDeleted, &et.ExamTypeCreatedAt, &et.ExamTypeUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("exam: scan type: %w", err)
		}
		list = append(list, et)
	}
	return list, rows.Err()
}

// ListExamRooms returns all non-deleted exam rooms.
func (s *Service) ListExamRooms(ctx context.Context) ([]ExamRoom, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_room_id, exam_room_name, exam_room_location, exam_room_capacity,
		       exam_room_is_deleted, exam_room_created_at, exam_room_updated_at
		FROM exam_rooms
		WHERE exam_room_is_deleted = FALSE
		ORDER BY exam_room_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list rooms: %w", err)
	}
	defer rows.Close()
	var list []ExamRoom
	for rows.Next() {
		var r ExamRoom
		if err := rows.Scan(
			&r.ExamRoomID, &r.ExamRoomName, &r.ExamRoomLocation, &r.ExamRoomCapacity,
			&r.ExamRoomIsDeleted, &r.ExamRoomCreatedAt, &r.ExamRoomUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("exam: scan room: %w", err)
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// ListExamSessions returns open, non-deleted sessions ordered by date.
func (s *Service) ListExamSessions(ctx context.Context) ([]ExamSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id, exam_session_exam_room_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_is_deleted, exam_session_created_at, exam_session_updated_at
		FROM exam_sessions
		WHERE exam_session_is_deleted = FALSE AND exam_session_status = 'open'
		ORDER BY exam_session_date ASC, exam_session_shift ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	defer rows.Close()
	var list []ExamSession
	for rows.Next() {
		var sess ExamSession
		if err := rows.Scan(
			&sess.ExamSessionID, &sess.ExamSessionExamTypeID, &sess.ExamSessionExamRoomID,
			&sess.ExamSessionDate, &sess.ExamSessionShift, &sess.ExamSessionCapacity,
			&sess.ExamSessionFee, &sess.ExamSessionRegistrationDeadline, &sess.ExamSessionStatus,
			&sess.ExamSessionIsDeleted, &sess.ExamSessionCreatedAt, &sess.ExamSessionUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("exam: scan session: %w", err)
		}
		list = append(list, sess)
	}
	return list, rows.Err()
}

// GetExamSession returns a single session by ID.
func (s *Service) GetExamSession(ctx context.Context, sessionID int64) (*ExamSession, error) {
	var sess ExamSession
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id, exam_session_exam_room_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_is_deleted, exam_session_created_at, exam_session_updated_at
		FROM exam_sessions
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, sessionID,
	).Scan(
		&sess.ExamSessionID, &sess.ExamSessionExamTypeID, &sess.ExamSessionExamRoomID,
		&sess.ExamSessionDate, &sess.ExamSessionShift, &sess.ExamSessionCapacity,
		&sess.ExamSessionFee, &sess.ExamSessionRegistrationDeadline, &sess.ExamSessionStatus,
		&sess.ExamSessionIsDeleted, &sess.ExamSessionCreatedAt, &sess.ExamSessionUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("exam session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("exam: get session: %w", err)
	}
	return &sess, nil
}

// ListExamSeats returns available seats for a session.
func (s *Service) ListExamSeats(ctx context.Context, sessionID int64) ([]ExamSeat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_seat_id, exam_seat_exam_session_id, exam_seat_number,
		       exam_seat_status, exam_seat_held_until, exam_seat_is_deleted,
		       exam_seat_created_at, exam_seat_updated_at
		FROM exam_seats
		WHERE exam_seat_exam_session_id = $1
		  AND exam_seat_status = 'available'
		  AND exam_seat_is_deleted = FALSE
		ORDER BY exam_seat_number ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("exam: list seats: %w", err)
	}
	defer rows.Close()
	var list []ExamSeat
	for rows.Next() {
		var seat ExamSeat
		if err := rows.Scan(
			&seat.ExamSeatID, &seat.ExamSeatExamSessionID, &seat.ExamSeatNumber,
			&seat.ExamSeatStatus, &seat.ExamSeatHeldUntil, &seat.ExamSeatIsDeleted,
			&seat.ExamSeatCreatedAt, &seat.ExamSeatUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("exam: scan seat: %w", err)
		}
		list = append(list, seat)
	}
	return list, rows.Err()
}

// GenerateExamSeats pre-creates seats 1..capacity for a session (idempotent).
func (s *Service) GenerateExamSeats(ctx context.Context, sessionID int64) (int, error) {
	var capacity int
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_session_capacity FROM exam_sessions
		 WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, sessionID,
	).Scan(&capacity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("exam session not found")
		}
		return 0, fmt.Errorf("exam: fetch capacity: %w", err)
	}
	created := 0
	for i := 1; i <= capacity; i++ {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
			VALUES ($1, $2, 'available')
			ON CONFLICT DO NOTHING`, sessionID, i)
		if err != nil {
			return created, fmt.Errorf("exam: insert seat %d: %w", i, err)
		}
		n, _ := res.RowsAffected()
		created += int(n)
	}
	return created, nil
}
