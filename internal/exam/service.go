package exam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/dto/response"
	"github.com/hskvinh/backend/internal/exam/enum"
)

// Service handles exam types, rooms, sessions, and seat generation.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
}

// NewService creates an exam Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

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

// ListExamSessions returns all non-deleted sessions as DTOs.
func (s *Service) ListExamSessions(ctx context.Context) ([]response.ExamSessionResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at
		FROM exam_sessions
		WHERE exam_session_is_deleted = FALSE
		  AND exam_session_status != 'cancelled'
		  AND exam_session_publication_status = 'published'
		  AND (exam_session_publish_at IS NULL OR exam_session_publish_at <= NOW())
		ORDER BY exam_session_date ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	defer rows.Close()
	var list []response.ExamSessionResponseDTO
	for rows.Next() {
		var e ExamSessionEntity
		if err := rows.Scan(
			&e.ExamSessionID, &e.ExamSessionExamTypeID,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
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
		SELECT exam_session_id, exam_session_exam_type_id,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at
		FROM exam_sessions
		WHERE exam_session_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status != 'cancelled'
		  AND exam_session_publication_status = 'published'
		  AND (exam_session_publish_at IS NULL OR exam_session_publish_at <= NOW())`, id,
	).Scan(
		&e.ExamSessionID, &e.ExamSessionExamTypeID,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
		&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamSessionResponseDTO{}, errors.New("Không tìm thấy ca thi")
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
		FROM exam_seats es
		JOIN exam_sessions sess ON sess.exam_session_id = es.exam_seat_exam_session_id
		WHERE es.exam_seat_exam_session_id = $1
		  AND es.exam_seat_is_deleted = FALSE
		  AND sess.exam_session_is_deleted = FALSE
		  AND sess.exam_session_status != 'cancelled'
		  AND sess.exam_session_publication_status = 'published'
		  AND (sess.exam_session_publish_at IS NULL OR sess.exam_session_publish_at <= NOW())
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
// actorID is the admin account that triggered the generation.
func (s *Service) GenerateExamSeats(ctx context.Context, sessionID int64, actorID int64) (int, error) {
	var capacity int
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_session_capacity FROM exam_sessions
		 WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, sessionID,
	).Scan(&capacity); errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("Không tìm thấy ca thi")
	} else if err != nil {
		return 0, fmt.Errorf("exam: get capacity: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
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

	newVal, _ := json.Marshal(map[string]any{"session_id": sessionID, "created_seats": n})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamSeat.String(),
		EntityID:       sessionID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("exam: commit: %w", err)
	}
	return int(n), nil
}

// ListExamSessionsFiltered returns sessions matching optional filters.
// All params are optional — zero value means "no filter".
func (s *Service) ListExamSessionsFiltered(ctx context.Context, f examreq.ListExamSessionsFilterDTO) ([]response.ExamSessionResponseDTO, error) {
	// Build dynamic WHERE clause — avoid string interpolation, use $N args
	args := []any{}
	where := "WHERE es.exam_session_is_deleted = FALSE AND es.exam_session_status != 'cancelled' AND es.exam_session_publication_status = 'published' AND (es.exam_session_publish_at IS NULL OR es.exam_session_publish_at <= NOW())"
	n := 1

	if f.ExamTypeID > 0 {
		where += fmt.Sprintf(" AND es.exam_session_exam_type_id = $%d", n)
		args = append(args, f.ExamTypeID)
		n++
	}
	if !f.From.IsZero() {
		where += fmt.Sprintf(" AND es.exam_session_date >= $%d", n)
		args = append(args, f.From.Format("2006-01-02"))
		n++
	}
	if !f.To.IsZero() {
		where += fmt.Sprintf(" AND es.exam_session_date <= $%d", n)
		args = append(args, f.To.Format("2006-01-02"))
		n++
	}
	_ = n // province_id is retained for query compatibility until location mapping is defined.

	query := `
		SELECT es.exam_session_id, es.exam_session_exam_type_id,
		       es.exam_session_date, es.exam_session_shift, es.exam_session_capacity, es.exam_session_fee,
		       es.exam_session_registration_deadline, es.exam_session_status,
		       es.exam_session_publication_status, es.exam_session_publish_at, es.exam_session_check_in_at
		FROM exam_sessions es
		` + where + `
		ORDER BY es.exam_session_date ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions filtered: %w", err)
	}
	defer rows.Close()

	var list []response.ExamSessionResponseDTO
	for rows.Next() {
		var e ExamSessionEntity
		if err := rows.Scan(
			&e.ExamSessionID, &e.ExamSessionExamTypeID,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
		); err != nil {
			return nil, fmt.Errorf("exam: scan session filtered: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e))
	}
	return list, rows.Err()
}

// CancelExamSession marks a session as cancelled (admin only).
func (s *Service) CancelExamSession(ctx context.Context, sessionID, actorID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT exam_session_status FROM exam_sessions
		 WHERE exam_session_id=$1 AND exam_session_is_deleted=FALSE FOR UPDATE`, sessionID,
	).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy ca thi")
	} else if err != nil {
		return fmt.Errorf("exam: lock session: %w", err)
	}
	if status == "cancelled" {
		return errors.New("Ca thi đã bị hủy từ trước")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_sessions SET exam_session_status='cancelled', exam_session_updated_at=NOW()
		 WHERE exam_session_id=$1`, sessionID,
	); err != nil {
		return fmt.Errorf("exam: cancel session: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": status})
	newVal, _ := json.Marshal(map[string]any{"status": "cancelled"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionDelete.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// BatchCreateExamSessions creates multiple sessions in a single transaction.
func (s *Service) BatchCreateExamSessions(ctx context.Context, reqs []examreq.CreateExamSessionRequestDTO, actorID int64) ([]response.ExamSessionResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	list := make([]response.ExamSessionResponseDTO, 0, len(reqs))
	for _, r := range reqs {
		deadline, err := time.Parse(time.RFC3339, r.RegistrationDeadline)
		if err != nil {
			return nil, fmt.Errorf("exam: parse deadline '%s': %w", r.RegistrationDeadline, err)
		}
		publicationStatus := enum.PublicationStatusPublished
		if r.PublicationStatus != "" {
			publicationStatus = enum.ExamSessionPublicationStatus(r.PublicationStatus)
		}
		if !publicationStatus.Valid() {
			return nil, errors.New("Trạng thái công khai của ca thi không hợp lệ")
		}
		publishAt, err := parseOptionalRFC3339(r.PublishAt)
		if err != nil {
			return nil, fmt.Errorf("exam: parse publish_at: %w", err)
		}
		checkInAt, err := parseOptionalRFC3339(r.CheckInAt)
		if err != nil {
			return nil, fmt.Errorf("exam: parse check_in_at: %w", err)
		}
		if publicationStatus == enum.PublicationStatusScheduled {
			if publishAt == nil || !publishAt.After(time.Now()) {
				return nil, errors.New("Ca thi hẹn giờ phải có publish_at trong tương lai")
			}
		}
		if publicationStatus == enum.PublicationStatusDraft && publishAt != nil {
			return nil, errors.New("Ca thi lưu nháp không được có publish_at")
		}
		if publicationStatus == enum.PublicationStatusPublished && publishAt != nil && publishAt.After(time.Now()) {
			return nil, errors.New("Ca thi công khai không được có publish_at trong tương lai")
		}
		var e ExamSessionEntity
		err = tx.QueryRowContext(ctx, `
			INSERT INTO exam_sessions (
				exam_session_exam_type_id,
				exam_session_date, exam_session_shift, exam_session_capacity,
				exam_session_fee, exam_session_registration_deadline, exam_session_status,
				exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at
			) VALUES ($1,$2::DATE,$3,$4,$5,$6,'open',$7,$8,$9)
			RETURNING exam_session_id, exam_session_exam_type_id,
			          exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
			          exam_session_registration_deadline, exam_session_status,
			          exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at`,
			r.ExamTypeID, r.Date, r.Shift, r.Capacity, r.Fee, deadline, publicationStatus,
			publishAt, checkInAt,
		).Scan(
			&e.ExamSessionID, &e.ExamSessionExamTypeID,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
		)
		if err != nil {
			return nil, fmt.Errorf("exam: batch create session: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e))
	}

	newVal, _ := json.Marshal(map[string]any{"batch_count": len(reqs)})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       0,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("exam: commit batch: %w", err)
	}
	return list, nil
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("giá trị phải là RFC3339: %w", err)
	}
	return &t, nil
}
