package exam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hskvinh/backend/internal/exam/dto/response"
)

// Service handles exam types, rooms, sessions, and seat generation.
type Service struct{ db *sql.DB }

// NewService creates an exam Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListExamTypes returns all non-deleted exam types as DTOs.
func (s *Service) ListExamTypes(ctx context.Context) ([]response.ExamTypeResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_type_id, exam_type_code, exam_type_name, exam_type_description
		FROM exam_types WHERE exam_type_is_deleted = FALSE ORDER BY exam_type_code ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list types: %w", err)
	}
	defer rows.Close()
	var list []response.ExamTypeResponseDTO
	for rows.Next() {
		var e ExamTypeEntity
		if err := rows.Scan(&e.ExamTypeID, &e.ExamTypeCode, &e.ExamTypeName, &e.ExamTypeDescription); err != nil {
			return nil, fmt.Errorf("exam: scan type: %w", err)
		}
		list = append(list, ToExamTypeResponseDTO(e))
	}
	return list, rows.Err()
}

// ListExamRooms returns all non-deleted exam rooms as DTOs.
func (s *Service) ListExamRooms(ctx context.Context) ([]response.ExamRoomResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_room_id, exam_room_name, exam_room_location, exam_room_capacity
		FROM exam_rooms WHERE exam_room_is_deleted = FALSE ORDER BY exam_room_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list rooms: %w", err)
	}
	defer rows.Close()
	var list []response.ExamRoomResponseDTO
	for rows.Next() {
		var e ExamRoomEntity
		if err := rows.Scan(&e.ExamRoomID, &e.ExamRoomName, &e.ExamRoomLocation, &e.ExamRoomCapacity); err != nil {
			return nil, fmt.Errorf("exam: scan room: %w", err)
		}
		list = append(list, ToExamRoomResponseDTO(e))
	}
	return list, rows.Err()
}

// ListExamSessions returns all non-deleted sessions as DTOs.
func (s *Service) ListExamSessions(ctx context.Context) ([]response.ExamSessionResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id, exam_session_exam_room_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status
		FROM exam_sessions WHERE exam_session_is_deleted = FALSE ORDER BY exam_session_date ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	defer rows.Close()
	var list []response.ExamSessionResponseDTO
	for rows.Next() {
		var e ExamSessionEntity
		if err := rows.Scan(
			&e.ExamSessionID, &e.ExamSessionExamTypeID, &e.ExamSessionExamRoomID,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
		); err != nil {
			return nil, fmt.Errorf("exam: scan session: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e))
	}
	return list, rows.Err()
}

// GetExamSession returns a single session DTO by ID.
func (s *Service) GetExamSession(ctx context.Context, id int64) (response.ExamSessionResponseDTO, error) {
	var e ExamSessionEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id, exam_session_exam_room_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status
		FROM exam_sessions WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, id,
	).Scan(
		&e.ExamSessionID, &e.ExamSessionExamTypeID, &e.ExamSessionExamRoomID,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamSessionResponseDTO{}, errors.New("exam session not found")
	}
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: get session: %w", err)
	}
	return ToExamSessionResponseDTO(e), nil
}

// ListExamSeats returns seats for a given session as DTOs.
func (s *Service) ListExamSeats(ctx context.Context, sessionID int64) ([]response.ExamSeatResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_seat_id, exam_seat_exam_session_id, exam_seat_number, exam_seat_status, exam_seat_held_until
		FROM exam_seats
		WHERE exam_seat_exam_session_id = $1 AND exam_seat_is_deleted = FALSE
		ORDER BY exam_seat_number ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("exam: list seats: %w", err)
	}
	defer rows.Close()
	var list []response.ExamSeatResponseDTO
	for rows.Next() {
		var e ExamSeatEntity
		if err := rows.Scan(
			&e.ExamSeatID, &e.ExamSeatExamSessionID, &e.ExamSeatNumber,
			&e.ExamSeatStatus, &e.ExamSeatHeldUntil,
		); err != nil {
			return nil, fmt.Errorf("exam: scan seat: %w", err)
		}
		list = append(list, ToExamSeatResponseDTO(e))
	}
	return list, rows.Err()
}

// GenerateExamSeats bulk-creates seats for a session up to its capacity.
func (s *Service) GenerateExamSeats(ctx context.Context, sessionID int64) (int, error) {
	var capacity int
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_session_capacity FROM exam_sessions
		 WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, sessionID,
	).Scan(&capacity); errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("exam session not found")
	} else if err != nil {
		return 0, fmt.Errorf("exam: get capacity: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
		SELECT $1, gs, 'available'
		FROM generate_series(1,$2) AS gs
		ON CONFLICT (exam_seat_exam_session_id, exam_seat_number) DO NOTHING`,
		sessionID, capacity,
	)
	if err != nil {
		return 0, fmt.Errorf("exam: generate seats: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
