package exam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/cache"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/dto/response"
	"github.com/hskvinh/backend/internal/exam/enum"
	"github.com/lib/pq"
)

const (
	cacheKeyExamTypes       = "hsk:exam:types"
	cacheKeyExamSessionsPub = "hsk:exam:sessions:published"
	cachePrefixExamSession  = "hsk:exam:session:"
	duplicateNameSuffix     = " (Bản sao)"
)

var sessionCopySuffixRegex = regexp.MustCompile(`(?i)\s*\(copy\)+$`)

func formatDuplicateSessionName(name string) string {
	cleaned := sessionCopySuffixRegex.ReplaceAllString(strings.TrimSpace(name), "")
	return cleaned + " (Copy)"
}

// Service handles exam types, rooms, sessions, and seat generation.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	cacheSvc *cache.Service
}

var (
	ErrExamSessionNotFound      = errors.New("Không tìm thấy ca thi")
	ErrExamPeriodNotFound       = errors.New("Không tìm thấy kỳ thi")
	ErrNonDraftDateModification = errors.New("Kỳ thi đã mở đăng ký hoặc đã công khai. Không thể thay đổi trực tiếp ngày thi hoặc hạn đăng ký. Vui lòng sử dụng quy trình dời lịch (Reschedule) để cảnh báo và gửi thông báo tới thí sinh.")
	// ErrExamPeriodCodeExists is returned when the requested period code collides with an existing one.
	ErrExamPeriodCodeExists = errors.New("Mã kỳ thi đã tồn tại. Vui lòng chọn mã khác.")
	// ErrSessionScheduleIncomplete blocks publishing a session whose date/deadline/capacity is still NULL
	// (typical right after duplicating a period or an exam session).
	ErrSessionScheduleIncomplete = errors.New("Ca thi chưa có ngày thi, số lượng chỗ hoặc hạn đăng ký. Vui lòng cập nhật lịch thi trước khi công khai.")
	// ErrSessionNotEditable blocks direct schedule/fee edits once a session left draft or has registrations.
	ErrSessionNotEditable = errors.New("Ca thi đã công khai hoặc đã có thí sinh đăng ký. Không thể sửa trực tiếp ngày thi, lệ phí hoặc sức chứa; vui lòng sử dụng quy trình dời lịch (Reschedule).")
	// ErrExamSessionCancelled blocks edits on cancelled sessions.
	ErrExamSessionCancelled = errors.New("Không thể chỉnh sửa ca thi đã hủy")
	// ErrParentPeriodClosed blocks duplicating an exam session into a closed/cancelled/completed period.
	ErrParentPeriodClosed  = errors.New("Đợt thi đã đóng, đã hủy hoặc đã hoàn thành; không thể nhân bản thêm ca thi vào đợt này")
	errExamSessionNotFound = ErrExamSessionNotFound
)

// ValidationError carries a user-facing (Vietnamese) input validation message.
// Handlers map it to HTTP 400.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// IsValidationError reports whether err (or any wrapped error) is a ValidationError.
func IsValidationError(err error) bool {
	var v ValidationError
	return errors.As(err, &v)
}

// isUniqueViolation reports whether err is a PostgreSQL unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// NewService creates an exam Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

// SetCache injects the Redis cache service.
func (s *Service) SetCache(cacheSvc *cache.Service) {
	s.cacheSvc = cacheSvc
}

// ClearCache clears all exam-related caches (types, published sessions, individual sessions).
func (s *Service) ClearCache(ctx context.Context) error {
	if s.cacheSvc == nil {
		return nil
	}
	_ = s.cacheSvc.Delete(ctx, cacheKeyExamTypes, cacheKeyExamSessionsPub)
	_ = s.cacheSvc.DeletePrefix(ctx, "hsk:exam:sessions:filt:")
	return s.cacheSvc.DeletePrefix(ctx, cachePrefixExamSession)
}

func (s *Service) invalidateSessionCache(ctx context.Context, sessionIDs ...int64) {
	if s.cacheSvc == nil {
		return
	}
	keys := []string{cacheKeyExamSessionsPub}
	for _, id := range sessionIDs {
		keys = append(keys, fmt.Sprintf("%s%d", cachePrefixExamSession, id))
	}
	_ = s.cacheSvc.Delete(ctx, keys...)
	_ = s.cacheSvc.DeletePrefix(ctx, "hsk:exam:sessions:filt:")
}

// ListExamTypes returns all non-deleted exam types as DTOs.
func (s *Service) ListExamTypes(ctx context.Context) ([]response.ExamTypeResponseDTO, error) {
	if s.cacheSvc != nil {
		var cached []response.ExamTypeResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKeyExamTypes, &cached); hit {
			return cached, nil
		}
	}

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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKeyExamTypes, list, 24*time.Hour)
	}
	return list, nil
}

// ListExamSessions returns all non-deleted sessions as DTOs.
func (s *Service) ListExamSessions(ctx context.Context) ([]response.ExamSessionResponseDTO, error) {
	if s.cacheSvc != nil {
		var cached []response.ExamSessionResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKeyExamSessionsPub, &cached); hit {
			return cached, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id,
		       exam_session_name, exam_session_description,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		       exam_session_duplicated_from_id
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
			&e.ExamSessionName, &e.ExamSessionDescription,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
			&e.ExamSessionDuplicatedFromID,
		); err != nil {
			return nil, fmt.Errorf("exam: scan session: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		// Apply ±35% dynamic jitter to prevent simultaneous expiry of batch-published sessions
		_ = s.cacheSvc.SetWithJitter(ctx, cacheKeyExamSessionsPub, list, 10*time.Minute, 0.35)
	}
	return list, nil
}

// GetExamSession returns a single session DTO by ID.
func (s *Service) GetExamSession(ctx context.Context, id int64) (response.ExamSessionResponseDTO, error) {
	cacheKey := fmt.Sprintf("%s%d", cachePrefixExamSession, id)
	if s.cacheSvc != nil {
		var cached response.ExamSessionResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	var e ExamSessionEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id,
		       exam_session_name, exam_session_description,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		       exam_session_duplicated_from_id
		FROM exam_sessions
		WHERE exam_session_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status != 'cancelled'
		  AND exam_session_publication_status = 'published'
		  AND (exam_session_publish_at IS NULL OR exam_session_publish_at <= NOW())`, id,
	).Scan(
		&e.ExamSessionID, &e.ExamSessionExamTypeID,
		&e.ExamSessionName, &e.ExamSessionDescription,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
		&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
		&e.ExamSessionDuplicatedFromID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if s.cacheSvc != nil {
			s.cacheSvc.SetSentinel(ctx, cacheKey, 1*time.Minute)
		}
		return response.ExamSessionResponseDTO{}, errors.New("Không tìm thấy ca thi")
	}
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: get session: %w", err)
	}
	dto := ToExamSessionResponseDTO(e)
	if s.cacheSvc != nil {
		// Apply ±35% dynamic jitter to prevent simultaneous expiry
		_ = s.cacheSvc.SetWithJitter(ctx, cacheKey, dto, 10*time.Minute, 0.35)
	}
	return dto, nil
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
	return s.listExamSessionsFiltered(ctx, f, true)
}

// ListAdminExamSessionsFiltered returns all non-deleted sessions for administrators.
func (s *Service) ListAdminExamSessionsFiltered(ctx context.Context, f examreq.ListExamSessionsFilterDTO) ([]response.ExamSessionResponseDTO, error) {
	return s.listExamSessionsFiltered(ctx, f, false)
}

func (s *Service) listExamSessionsFiltered(ctx context.Context, f examreq.ListExamSessionsFilterDTO, publicOnly bool) ([]response.ExamSessionResponseDTO, error) {
	cacheKey := fmt.Sprintf("hsk:exam:sessions:filt:%t:%d:%s:%s",
		publicOnly,
		f.ExamTypeID,
		f.From.Format("2006-01-02"),
		f.To.Format("2006-01-02"),
	)
	if s.cacheSvc != nil {
		var cached []response.ExamSessionResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	// Build dynamic WHERE clause — avoid string interpolation, use $N args
	args := []any{}
	where := "WHERE es.exam_session_is_deleted = FALSE"
	if publicOnly {
		where += " AND es.exam_session_status != 'cancelled' AND es.exam_session_publication_status = 'published' AND (es.exam_session_publish_at IS NULL OR es.exam_session_publish_at <= NOW())"
	}
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
		       es.exam_session_name, es.exam_session_description,
		       es.exam_session_date, es.exam_session_shift, es.exam_session_capacity, es.exam_session_fee,
		       es.exam_session_registration_deadline, es.exam_session_status,
		       es.exam_session_publication_status, es.exam_session_publish_at, es.exam_session_check_in_at,
		       es.exam_session_duplicated_from_id
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
			&e.ExamSessionName, &e.ExamSessionDescription,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
			&e.ExamSessionDuplicatedFromID,
		); err != nil {
			return nil, fmt.Errorf("exam: scan session filtered: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.SetWithJitter(ctx, cacheKey, list, 10*time.Minute, 0.35)
	}
	return list, nil
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

	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateSessionCache(ctx, sessionID)
	return nil
}

// UpdatePublicationStatus changes the public visibility state of an exam session.
func (s *Service) UpdatePublicationStatus(ctx context.Context, sessionID, actorID int64, req examreq.UpdatePublicationStatusRequestDTO) (response.UpdatePublicationStatusResponseDTO, error) {
	publicationStatus := enum.ExamSessionPublicationStatus(req.PublicationStatus)
	if !publicationStatus.Valid() {
		return response.UpdatePublicationStatusResponseDTO{}, errors.New("Trạng thái phát hành không hợp lệ")
	}

	publishAt, err := parseOptionalRFC3339(req.PublishAt)
	if err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, fmt.Errorf("exam: parse publication time: %w", err)
	}
	if publicationStatus == enum.PublicationStatusPublished || publicationStatus == enum.PublicationStatusDraft {
		publishAt = nil
	}
	if err := validatePublicationStatus(publicationStatus, publishAt); err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, fmt.Errorf("exam: begin publication tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var oldStatus, operationalStatus string
	var sessionDate, registrationDeadline *time.Time
	var capacity *int
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_session_publication_status, exam_session_status,
		       exam_session_date, exam_session_capacity, exam_session_registration_deadline
		FROM exam_sessions
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE
		FOR UPDATE`, sessionID).Scan(&oldStatus, &operationalStatus, &sessionDate, &capacity, &registrationDeadline); errors.Is(err, sql.ErrNoRows) {
		return response.UpdatePublicationStatusResponseDTO{}, errExamSessionNotFound
	} else if err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, fmt.Errorf("exam: lock publication session: %w", err)
	}
	if operationalStatus == enum.SessionStatusCancelled.String() {
		return response.UpdatePublicationStatusResponseDTO{}, errors.New("Không thể cập nhật trạng thái phát hành của ca thi đã hủy")
	}
	// A duplicated session starts with NULL date/deadline/capacity. It must never become visible
	// (published or scheduled) until the admin re-enters the schedule and slots.
	if publicationStatus != enum.PublicationStatusDraft && (sessionDate == nil || capacity == nil || *capacity <= 0 || registrationDeadline == nil) {
		return response.UpdatePublicationStatusResponseDTO{}, ErrSessionScheduleIncomplete
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_publication_status = $1,
		    exam_session_publish_at = $2,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $3 AND exam_session_is_deleted = FALSE`,
		publicationStatus, publishAt, sessionID,
	); err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, fmt.Errorf("exam: update publication status: %w", err)
	}

	oldValue, _ := json.Marshal(map[string]any{"exam_session_publication_status": oldStatus})
	newValue, _ := json.Marshal(map[string]any{
		"exam_session_publication_status": publicationStatus.String(),
		"exam_session_publish_at":         publishAt,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldValue,
		NewValue:       newValue,
	})

	if err := tx.Commit(); err != nil {
		return response.UpdatePublicationStatusResponseDTO{}, fmt.Errorf("exam: commit publication status: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)
	return response.UpdatePublicationStatusResponseDTO{
		Message:           "Cập nhật trạng thái phát hành thành công",
		PublicationStatus: publicationStatus.String(),
		PublishAt:         publishAt,
	}, nil
}

// PublishScheduledSessions atomically makes due scheduled sessions public.
func (s *Service) PublishScheduledSessions(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("exam: begin scheduled publication tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.QueryContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_publication_status = 'published',
		    exam_session_publish_at = NULL,
		    exam_session_updated_at = NOW()
		WHERE exam_session_publication_status = 'scheduled'
		  AND exam_session_publish_at IS NOT NULL
		  AND exam_session_publish_at <= NOW()
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status != 'cancelled'
		  AND exam_session_date IS NOT NULL
		  AND exam_session_capacity IS NOT NULL
		  AND exam_session_registration_deadline IS NOT NULL
		RETURNING exam_session_id`)
	if err != nil {
		return 0, fmt.Errorf("exam: publish scheduled sessions: %w", err)
	}
	defer rows.Close()

	var published int64
	for rows.Next() {
		var sessionID int64
		if err := rows.Scan(&sessionID); err != nil {
			return 0, fmt.Errorf("exam: scan published session: %w", err)
		}
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			Action:      auditenum.AuditActionUpdate.String(),
			EntityTable: auditenum.AuditEntityExamSession.String(),
			EntityID:    sessionID,
			NewValue:    json.RawMessage(`{"exam_session_publication_status":"published","exam_session_publish_at":null}`),
		})
		published++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("exam: iterate published sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("exam: commit scheduled publication: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return published, nil
}

func validatePublicationStatus(status enum.ExamSessionPublicationStatus, publishAt *time.Time) error {
	switch status {
	case enum.PublicationStatusScheduled:
		if publishAt == nil || !publishAt.After(time.Now()) {
			return errors.New("Ca thi hẹn giờ phải có exam_session_publish_at trong tương lai")
		}
	case enum.PublicationStatusDraft:
		if publishAt != nil {
			return errors.New("Ca thi lưu nháp không được có exam_session_publish_at")
		}
	case enum.PublicationStatusPublished:
		if publishAt != nil && publishAt.After(time.Now()) {
			return errors.New("Ca thi công khai không được có exam_session_publish_at trong tương lai")
		}
	}
	return nil
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
			return nil, fmt.Errorf("exam: parse exam_session_publish_at: %w", err)
		}
		checkInAt, err := parseOptionalRFC3339(r.CheckInAt)
		if err != nil {
			return nil, fmt.Errorf("exam: parse check_in_at: %w", err)
		}
		if publicationStatus == enum.PublicationStatusScheduled {
			if publishAt == nil || !publishAt.After(time.Now()) {
				return nil, errors.New("Ca thi hẹn giờ phải có exam_session_publish_at trong tương lai")
			}
		}
		if publicationStatus == enum.PublicationStatusDraft && publishAt != nil {
			return nil, errors.New("Ca thi lưu nháp không được có exam_session_publish_at")
		}
		if publicationStatus == enum.PublicationStatusPublished {
			publishAt = nil
		}
		if publicationStatus == enum.PublicationStatusPublished && publishAt != nil && publishAt.After(time.Now()) {
			return nil, errors.New("Ca thi công khai không được có exam_session_publish_at trong tương lai")
		}
		var e ExamSessionEntity
		err = tx.QueryRowContext(ctx, `
			INSERT INTO exam_sessions (
				exam_session_exam_type_id,
				exam_session_name,
				exam_session_date, exam_session_shift, exam_session_capacity,
				exam_session_fee, exam_session_registration_deadline, exam_session_status,
				exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at
			) VALUES (
				$1,
				(SELECT exam_type_name FROM exam_types WHERE exam_type_id = $1) || ' - ' || INITCAP($3),
				$2::DATE,$3,$4,$5,$6,'open',$7,$8,$9
			)
			RETURNING exam_session_id, exam_session_exam_type_id,
			          exam_session_name, exam_session_description,
			          exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
			          exam_session_registration_deadline, exam_session_status,
			          exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
			          exam_session_duplicated_from_id`,
			r.ExamTypeID, r.Date, r.Shift, r.Capacity, r.Fee, deadline, publicationStatus,
			publishAt, checkInAt,
		).Scan(
			&e.ExamSessionID, &e.ExamSessionExamTypeID,
			&e.ExamSessionName, &e.ExamSessionDescription,
			&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
			&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
			&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
			&e.ExamSessionDuplicatedFromID,
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
	s.invalidateSessionCache(ctx)
	return list, nil
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("exam_session_publish_at phải có định dạng RFC3339 hợp lệ")
	}
	return &t, nil
}

// AdminListExamPeriods lists exam periods with session counts and filtering.
func (s *Service) AdminListExamPeriods(ctx context.Context, f examreq.ListExamPeriodsFilterDTO) ([]response.ExamPeriodResponseDTO, error) {
	where := "WHERE ep.exam_period_is_deleted = FALSE"
	args := []any{}
	n := 1

	if f.Status != "" && f.Status != "all" {
		where += fmt.Sprintf(" AND ep.exam_period_status = $%d", n)
		args = append(args, f.Status)
		n++
	}
	if !f.From.IsZero() {
		where += fmt.Sprintf(" AND ep.exam_period_start_date >= $%d", n)
		args = append(args, f.From.Format("2006-01-02"))
		n++
	}
	if !f.To.IsZero() {
		where += fmt.Sprintf(" AND ep.exam_period_end_date <= $%d", n)
		args = append(args, f.To.Format("2006-01-02"))
		n++
	}

	query := `
		SELECT ep.exam_period_id, ep.exam_period_name, ep.exam_period_code,
		       ep.exam_period_description, ep.exam_period_instructions,
		       ep.exam_period_start_date, ep.exam_period_end_date,
		       ep.exam_period_registration_start, ep.exam_period_registration_deadline,
		       ep.exam_period_status, ep.exam_period_created_at, ep.exam_period_updated_at,
		       COUNT(es.exam_session_id) FILTER (WHERE es.exam_session_is_deleted = FALSE) AS session_count
		FROM exam_periods ep
		LEFT JOIN exam_sessions es ON es.exam_session_exam_period_id = ep.exam_period_id
		` + where + `
		GROUP BY ep.exam_period_id
		ORDER BY ep.exam_period_start_date DESC NULLS LAST, ep.exam_period_id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: list periods: %w", err)
	}
	defer rows.Close()

	list := make([]response.ExamPeriodResponseDTO, 0)
	for rows.Next() {
		var (
			e            ExamPeriodEntity
			sessionCount int
		)
		if err := rows.Scan(
			&e.ExamPeriodID, &e.ExamPeriodName, &e.ExamPeriodCode,
			&e.ExamPeriodDescription, &e.ExamPeriodInstructions,
			&e.ExamPeriodStartDate, &e.ExamPeriodEndDate,
			&e.ExamPeriodRegistrationStart, &e.ExamPeriodRegistrationDeadline,
			&e.ExamPeriodStatus, &e.ExamPeriodCreatedAt, &e.ExamPeriodUpdatedAt,
			&sessionCount,
		); err != nil {
			return nil, fmt.Errorf("exam: scan period: %w", err)
		}
		list = append(list, ToExamPeriodResponseDTO(e, sessionCount, nil))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// AdminGetExamPeriod returns details of a single exam period including all child sessions.
func (s *Service) AdminGetExamPeriod(ctx context.Context, periodID int64) (response.ExamPeriodResponseDTO, error) {
	var e ExamPeriodEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT ep.exam_period_id, ep.exam_period_name, ep.exam_period_code,
		       ep.exam_period_description, ep.exam_period_instructions,
		       ep.exam_period_start_date, ep.exam_period_end_date,
		       ep.exam_period_registration_start, ep.exam_period_registration_deadline,
		       ep.exam_period_status, ep.exam_period_created_at, ep.exam_period_updated_at
		FROM exam_periods ep
		WHERE ep.exam_period_id = $1 AND ep.exam_period_is_deleted = FALSE`,
		periodID,
	).Scan(
		&e.ExamPeriodID, &e.ExamPeriodName, &e.ExamPeriodCode,
		&e.ExamPeriodDescription, &e.ExamPeriodInstructions,
		&e.ExamPeriodStartDate, &e.ExamPeriodEndDate,
		&e.ExamPeriodRegistrationStart, &e.ExamPeriodRegistrationDeadline,
		&e.ExamPeriodStatus, &e.ExamPeriodCreatedAt, &e.ExamPeriodUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: get period: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_session_id, exam_session_exam_type_id,
		       exam_session_name, exam_session_description,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		       exam_session_duplicated_from_id
		FROM exam_sessions
		WHERE exam_session_exam_period_id = $1 AND exam_session_is_deleted = FALSE
		ORDER BY exam_session_date ASC NULLS LAST, exam_session_id ASC`,
		periodID,
	)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: get period sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]response.ExamSessionResponseDTO, 0)
	for rows.Next() {
		var se ExamSessionEntity
		se.ExamSessionExamPeriodID = &periodID
		if err := rows.Scan(
			&se.ExamSessionID, &se.ExamSessionExamTypeID,
			&se.ExamSessionName, &se.ExamSessionDescription,
			&se.ExamSessionDate, &se.ExamSessionShift, &se.ExamSessionCapacity, &se.ExamSessionFee,
			&se.ExamSessionRegistrationDeadline, &se.ExamSessionStatus,
			&se.ExamSessionPublicationStatus, &se.ExamSessionPublishAt, &se.ExamSessionCheckInAt,
			&se.ExamSessionDuplicatedFromID,
		); err != nil {
			return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: scan period session: %w", err)
		}
		sessions = append(sessions, ToExamSessionResponseDTO(se))
	}
	if err := rows.Err(); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	return ToExamPeriodResponseDTO(e, len(sessions), sessions), nil
}

// AdminCreateExamPeriod creates a new exam period in draft status.
func (s *Service) AdminCreateExamPeriod(ctx context.Context, req examreq.CreateExamPeriodRequestDTO, actorID int64) (response.ExamPeriodResponseDTO, error) {
	startDate, _ := parseDate(req.StartDate)
	endDate, _ := parseDate(req.EndDate)
	regStart, _ := parseOptionalRFC3339(req.RegistrationStart)
	regDeadline, _ := parseOptionalRFC3339(req.RegistrationDeadline)

	var desc, inst *string
	if req.Description != "" {
		desc = &req.Description
	}
	if req.Instructions != "" {
		inst = &req.Instructions
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO exam_periods (
			exam_period_name, exam_period_code, exam_period_description, exam_period_instructions,
			exam_period_start_date, exam_period_end_date,
			exam_period_registration_start, exam_period_registration_deadline,
			exam_period_status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'draft')
		RETURNING exam_period_id, exam_period_name, exam_period_code,
		          exam_period_description, exam_period_instructions,
		          exam_period_start_date, exam_period_end_date,
		          exam_period_registration_start, exam_period_registration_deadline,
		          exam_period_status, exam_period_created_at, exam_period_updated_at`,
		req.Name, req.Code, desc, inst, startDate, endDate, regStart, regDeadline,
	).Scan(
		&e.ExamPeriodID, &e.ExamPeriodName, &e.ExamPeriodCode,
		&e.ExamPeriodDescription, &e.ExamPeriodInstructions,
		&e.ExamPeriodStartDate, &e.ExamPeriodEndDate,
		&e.ExamPeriodRegistrationStart, &e.ExamPeriodRegistrationDeadline,
		&e.ExamPeriodStatus, &e.ExamPeriodCreatedAt, &e.ExamPeriodUpdatedAt,
	)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: insert period: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"id": e.ExamPeriodID, "code": e.ExamPeriodCode, "name": e.ExamPeriodName})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       e.ExamPeriodID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit create period: %w", err)
	}
	return ToExamPeriodResponseDTO(e, 0, nil), nil
}

// AdminDuplicateExamPeriod clones an exam period's configuration in ONE atomic transaction
// (POST /admin/exam-periods/:id/duplicate). The frontend only calls this once and then
// redirects to the edit screen of the returned new_period_id.
//
// Within the transaction:
//   - a new exam_period is created with status 'draft' and NULL dates;
//   - every non-cancelled child exam_session is copied (exam type, room, shift, capacity, fee)
//     with publication_status 'draft', and date / registration_deadline / publish_at /
//     check_in_at reset to NULL so the admin must re-enter the schedule before publishing;
//   - registrations, candidates and seats are NEVER copied.
func (s *Service) AdminDuplicateExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.DuplicateExamPeriodRequestDTO) (response.DuplicateExamPeriodResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: begin duplicate tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// FOR SHARE: the source must not change while being copied, but concurrent
	// duplicates of the same source are allowed.
	var src ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		SELECT exam_period_id, exam_period_name, exam_period_code,
		       exam_period_description, exam_period_instructions
		FROM exam_periods
		WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE
		FOR SHARE`,
		periodID,
	).Scan(
		&src.ExamPeriodID, &src.ExamPeriodName, &src.ExamPeriodCode,
		&src.ExamPeriodDescription, &src.ExamPeriodInstructions,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.DuplicateExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: lock source period: %w", err)
	}

	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		newName = truncateRunes(src.ExamPeriodName, 150-len([]rune(duplicateNameSuffix))) + duplicateNameSuffix
	}
	newCode := strings.TrimSpace(req.Code)
	if newCode == "" {
		suffix := fmt.Sprintf("-COPY-%d", time.Now().Unix())
		newCode = truncateRunes(src.ExamPeriodCode, 50-len(suffix)) + suffix
	}

	var newPeriod ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO exam_periods (
			exam_period_name, exam_period_code, exam_period_description,
			exam_period_instructions, exam_period_status
		) VALUES ($1, $2, $3, $4, 'draft')
		RETURNING exam_period_id, exam_period_name, exam_period_code,
		          exam_period_description, exam_period_instructions,
		          exam_period_start_date, exam_period_end_date,
		          exam_period_registration_start, exam_period_registration_deadline,
		          exam_period_status, exam_period_created_at, exam_period_updated_at`,
		newName, newCode, src.ExamPeriodDescription, src.ExamPeriodInstructions,
	).Scan(
		&newPeriod.ExamPeriodID, &newPeriod.ExamPeriodName, &newPeriod.ExamPeriodCode,
		&newPeriod.ExamPeriodDescription, &newPeriod.ExamPeriodInstructions,
		&newPeriod.ExamPeriodStartDate, &newPeriod.ExamPeriodEndDate,
		&newPeriod.ExamPeriodRegistrationStart, &newPeriod.ExamPeriodRegistrationDeadline,
		&newPeriod.ExamPeriodStatus, &newPeriod.ExamPeriodCreatedAt, &newPeriod.ExamPeriodUpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return response.DuplicateExamPeriodResponseDTO{}, ErrExamPeriodCodeExists
		}
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: insert cloned period: %w", err)
	}

	// Copy the configuration of every child session in a single set-based statement.
	// Schedule-related columns are explicitly NULL; registrations/seats are not touched.
	res, err := tx.ExecContext(ctx, `
		INSERT INTO exam_sessions (
			exam_session_exam_period_id,
			exam_session_exam_type_id,
			exam_session_exam_room_id,
			exam_session_name,
			exam_session_description,
			exam_session_shift,
			exam_session_capacity,
			exam_session_fee,
			exam_session_date,
			exam_session_registration_deadline,
			exam_session_status,
			exam_session_publication_status,
			exam_session_publish_at,
			exam_session_check_in_at
		)
		SELECT $1,
		       exam_session_exam_type_id,
		       exam_session_exam_room_id,
		       exam_session_name,
		       exam_session_description,
		       exam_session_shift,
		       exam_session_capacity,
		       exam_session_fee,
		       NULL, NULL, 'open', 'draft', NULL, NULL
		FROM exam_sessions
		WHERE exam_session_exam_period_id = $2
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status <> 'cancelled'
		ORDER BY exam_session_id`,
		newPeriod.ExamPeriodID, periodID,
	)
	if err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: clone child sessions: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: clone child sessions rows affected: %w", err)
	}
	copiedSessions := int(affected)

	newVal, _ := json.Marshal(map[string]any{
		"source_period_id":    periodID,
		"new_period_id":       newPeriod.ExamPeriodID,
		"duplicated_sessions": copiedSessions,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       newPeriod.ExamPeriodID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: commit duplicate: %w", err)
	}
	s.invalidateSessionCache(ctx)

	return response.DuplicateExamPeriodResponseDTO{
		NewPeriodID:        newPeriod.ExamPeriodID,
		DuplicatedSessions: copiedSessions,
		Period:             ToExamPeriodResponseDTO(newPeriod, copiedSessions, nil),
	}, nil
}

// AdminUpdateExamPeriod updates exam period information.
// Free editing is allowed only when status is 'draft'.
// Once published or registration_open onwards, modifying dates/deadlines returns ErrNonDraftDateModification.
func (s *Service) AdminUpdateExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.UpdateExamPeriodRequestDTO) (response.ExamPeriodResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin update tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		SELECT exam_period_id, exam_period_name, exam_period_code,
		       exam_period_description, exam_period_instructions,
		       exam_period_start_date, exam_period_end_date,
		       exam_period_registration_start, exam_period_registration_deadline,
		       exam_period_status, exam_period_created_at, exam_period_updated_at
		FROM exam_periods
		WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE
		FOR UPDATE`,
		periodID,
	).Scan(
		&e.ExamPeriodID, &e.ExamPeriodName, &e.ExamPeriodCode,
		&e.ExamPeriodDescription, &e.ExamPeriodInstructions,
		&e.ExamPeriodStartDate, &e.ExamPeriodEndDate,
		&e.ExamPeriodRegistrationStart, &e.ExamPeriodRegistrationDeadline,
		&e.ExamPeriodStatus, &e.ExamPeriodCreatedAt, &e.ExamPeriodUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: lock period: %w", err)
	}

	// Guard: from registration_open or published onwards, direct modification of dates/deadlines is forbidden
	if e.ExamPeriodStatus != enum.PeriodStatusDraft {
		if req.StartDate != nil || req.EndDate != nil || req.RegistrationStart != nil || req.RegistrationDeadline != nil {
			return response.ExamPeriodResponseDTO{}, ErrNonDraftDateModification
		}
	}

	name := e.ExamPeriodName
	if req.Name != nil && *req.Name != "" {
		name = *req.Name
	}
	desc := e.ExamPeriodDescription
	if req.Description != nil {
		desc = req.Description
	}
	inst := e.ExamPeriodInstructions
	if req.Instructions != nil {
		inst = req.Instructions
	}

	startDate := e.ExamPeriodStartDate
	endDate := e.ExamPeriodEndDate
	regStart := e.ExamPeriodRegistrationStart
	regDeadline := e.ExamPeriodRegistrationDeadline

	if e.ExamPeriodStatus == enum.PeriodStatusDraft {
		if req.StartDate != nil {
			d, err := parseDate(*req.StartDate)
			if err != nil {
				return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "định dạng ngày bắt đầu không hợp lệ (YYYY-MM-DD)"}
			}
			startDate = d
		}
		if req.EndDate != nil {
			d, err := parseDate(*req.EndDate)
			if err != nil {
				return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "định dạng ngày kết thúc không hợp lệ (YYYY-MM-DD)"}
			}
			endDate = d
		}
		if req.RegistrationStart != nil {
			t, err := parseOptionalRFC3339(*req.RegistrationStart)
			if err != nil {
				return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "thời gian bắt đầu đăng ký phải có định dạng RFC3339 hợp lệ"}
			}
			regStart = t
		}
		if req.RegistrationDeadline != nil {
			t, err := parseOptionalRFC3339(*req.RegistrationDeadline)
			if err != nil {
				return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "hạn đăng ký phải có định dạng RFC3339 hợp lệ"}
			}
			regDeadline = t
		}
	}

	var updated ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_name = $1,
		    exam_period_description = $2,
		    exam_period_instructions = $3,
		    exam_period_start_date = $4,
		    exam_period_end_date = $5,
		    exam_period_registration_start = $6,
		    exam_period_registration_deadline = $7,
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $8
		RETURNING exam_period_id, exam_period_name, exam_period_code,
		          exam_period_description, exam_period_instructions,
		          exam_period_start_date, exam_period_end_date,
		          exam_period_registration_start, exam_period_registration_deadline,
		          exam_period_status, exam_period_created_at, exam_period_updated_at`,
		name, desc, inst, startDate, endDate, regStart, regDeadline, periodID,
	).Scan(
		&updated.ExamPeriodID, &updated.ExamPeriodName, &updated.ExamPeriodCode,
		&updated.ExamPeriodDescription, &updated.ExamPeriodInstructions,
		&updated.ExamPeriodStartDate, &updated.ExamPeriodEndDate,
		&updated.ExamPeriodRegistrationStart, &updated.ExamPeriodRegistrationDeadline,
		&updated.ExamPeriodStatus, &updated.ExamPeriodCreatedAt, &updated.ExamPeriodUpdatedAt,
	)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: update period: %w", err)
	}

	// In draft mode, if the period's date or deadline is set, auto-populate child sessions that still have NULL
	if e.ExamPeriodStatus == enum.PeriodStatusDraft && (startDate != nil || regDeadline != nil) {
		_, _ = tx.ExecContext(ctx, `
			UPDATE exam_sessions
			SET exam_session_date = COALESCE(exam_session_date, $1),
			    exam_session_registration_deadline = COALESCE(exam_session_registration_deadline, $2),
			    exam_session_updated_at = NOW()
			WHERE exam_session_exam_period_id = $3
			  AND exam_session_publication_status = 'draft'
			  AND exam_session_is_deleted = FALSE`,
			startDate, regDeadline, periodID,
		)
	}

	newVal, _ := json.Marshal(map[string]any{"name": updated.ExamPeriodName, "status": updated.ExamPeriodStatus})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit update period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, 0, nil), nil
}

// AdminRescheduleExamPeriod postpones/reschedules an entire exam period.
// Cascades date updates to all sub-sessions while preserving registrations intact,
// records mandatory reason in audit log, and dispatches bulk notifications to affected candidates.
func (s *Service) AdminRescheduleExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.RescheduleExamPeriodRequestDTO) (response.RescheduleExamPeriodResponseDTO, error) {
	if req.Reason == "" {
		return response.RescheduleExamPeriodResponseDTO{}, errors.New("Lý do dời lịch là bắt buộc")
	}

	newStartDate, err := time.Parse("2006-01-02", req.NewStartDate)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, errors.New("Ngày thi mới không hợp lệ (định dạng YYYY-MM-DD)")
	}

	newEndDate := newStartDate
	if req.NewEndDate != "" {
		if ed, err := time.Parse("2006-01-02", req.NewEndDate); err == nil {
			newEndDate = ed
		}
	}

	var newDeadline *time.Time
	if req.NewRegistrationDeadline != "" {
		newDeadline, _ = parseOptionalRFC3339(req.NewRegistrationDeadline)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: begin reschedule tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var src ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		SELECT exam_period_id, exam_period_name, exam_period_code,
		       exam_period_start_date, exam_period_end_date,
		       exam_period_registration_deadline, exam_period_status
		FROM exam_periods
		WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE
		FOR UPDATE`,
		periodID,
	).Scan(
		&src.ExamPeriodID, &src.ExamPeriodName, &src.ExamPeriodCode,
		&src.ExamPeriodStartDate, &src.ExamPeriodEndDate,
		&src.ExamPeriodRegistrationDeadline, &src.ExamPeriodStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.RescheduleExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: lock reschedule period: %w", err)
	}

	if src.ExamPeriodStatus == enum.PeriodStatusCancelled {
		return response.RescheduleExamPeriodResponseDTO{}, errors.New("Không thể dời lịch kỳ thi đã bị hủy")
	}

	// Calculate date offset days
	daysOffset := req.DaysOffset
	if daysOffset == 0 && src.ExamPeriodStartDate != nil {
		daysOffset = int(newStartDate.Sub(*src.ExamPeriodStartDate).Hours() / 24)
	}

	// Update exam period
	var updated ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_start_date = $1,
		    exam_period_end_date = $2,
		    exam_period_registration_deadline = COALESCE($3, exam_period_registration_deadline),
		    exam_period_status = 'rescheduled',
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $4
		RETURNING exam_period_id, exam_period_name, exam_period_code,
		          exam_period_description, exam_period_instructions,
		          exam_period_start_date, exam_period_end_date,
		          exam_period_registration_start, exam_period_registration_deadline,
		          exam_period_status, exam_period_created_at, exam_period_updated_at`,
		newStartDate, newEndDate, newDeadline, periodID,
	).Scan(
		&updated.ExamPeriodID, &updated.ExamPeriodName, &updated.ExamPeriodCode,
		&updated.ExamPeriodDescription, &updated.ExamPeriodInstructions,
		&updated.ExamPeriodStartDate, &updated.ExamPeriodEndDate,
		&updated.ExamPeriodRegistrationStart, &updated.ExamPeriodRegistrationDeadline,
		&updated.ExamPeriodStatus, &updated.ExamPeriodCreatedAt, &updated.ExamPeriodUpdatedAt,
	)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: update rescheduled period: %w", err)
	}

	// Cascade update to all active sub-sessions: dates shift by daysOffset or set to newStartDate
	res, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_date = COALESCE(exam_session_date + ($1 * INTERVAL '1 day'), $2::DATE),
		    exam_session_registration_deadline = COALESCE($3, exam_session_registration_deadline),
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $4
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status != 'cancelled'`,
		daysOffset, newStartDate, newDeadline, periodID,
	)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: cascade reschedule to sessions: %w", err)
	}
	sessionsCount, _ := res.RowsAffected()

	// Mandatory bulk notification to all registered candidates under this period's sessions
	notifRes, err := tx.ExecContext(ctx, `
		INSERT INTO notifications (
			notification_account_id, notification_channel, notification_content, notification_status
		)
		SELECT DISTINCT c.candidate_account_id, 'email',
		       'Thông báo dời lịch thi: Kỳ thi "' || $1 || '" đã được dời ngày thi sang ' || $2 || '. Lý do: ' || $3,
		       'pending'
		FROM exam_registrations er
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		WHERE es.exam_session_exam_period_id = $4
		  AND er.exam_registration_status IN ('confirmed', 'pending_payment')
		  AND er.exam_registration_is_deleted = FALSE`,
		src.ExamPeriodName, newStartDate.Format("02/01/2006"), req.Reason, periodID,
	)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: insert bulk notifications: %w", err)
	}
	candidatesAlerted, _ := notifRes.RowsAffected()

	// Audit logging with old and new values including mandatory reason
	oldVal, _ := json.Marshal(map[string]any{
		"start_date": src.ExamPeriodStartDate,
		"end_date":   src.ExamPeriodEndDate,
		"deadline":   src.ExamPeriodRegistrationDeadline,
		"status":     src.ExamPeriodStatus,
	})
	newVal, _ := json.Marshal(map[string]any{
		"new_start_date":     newStartDate,
		"new_end_date":       newEndDate,
		"new_deadline":       newDeadline,
		"reason":             req.Reason,
		"days_offset":        daysOffset,
		"candidates_alerted": candidatesAlerted,
		"status":             "rescheduled",
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: commit reschedule: %w", err)
	}
	s.invalidateSessionCache(ctx)

	return response.RescheduleExamPeriodResponseDTO{
		PeriodID:          periodID,
		UpdatedSessions:   int(sessionsCount),
		CandidatesAlerted: int(candidatesAlerted),
		Reason:            req.Reason,
		Period:            ToExamPeriodResponseDTO(updated, int(sessionsCount), nil),
	}, nil
}

// AdminCloseRegistration performs manual early closure of an exam period before the established deadline.
func (s *Service) AdminCloseRegistration(ctx context.Context, periodID, actorID int64, req examreq.CloseRegistrationRequestDTO) (response.ExamPeriodResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin close registration tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var updated ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'registration_closed',
		    exam_period_registration_deadline = LEAST(exam_period_registration_deadline, NOW()),
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE
		RETURNING exam_period_id, exam_period_name, exam_period_code,
		          exam_period_description, exam_period_instructions,
		          exam_period_start_date, exam_period_end_date,
		          exam_period_registration_start, exam_period_registration_deadline,
		          exam_period_status, exam_period_created_at, exam_period_updated_at`,
		periodID,
	).Scan(
		&updated.ExamPeriodID, &updated.ExamPeriodName, &updated.ExamPeriodCode,
		&updated.ExamPeriodDescription, &updated.ExamPeriodInstructions,
		&updated.ExamPeriodStartDate, &updated.ExamPeriodEndDate,
		&updated.ExamPeriodRegistrationStart, &updated.ExamPeriodRegistrationDeadline,
		&updated.ExamPeriodStatus, &updated.ExamPeriodCreatedAt, &updated.ExamPeriodUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: close period registration: %w", err)
	}

	// Close open sessions under this period
	_, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'closed',
		    exam_session_registration_deadline = LEAST(exam_session_registration_deadline, NOW()),
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status = 'open'`,
		periodID,
	)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: close child sessions registration: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"period_id": periodID,
		"reason":    req.Reason,
		"status":    "registration_closed",
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit close registration: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, 0, nil), nil
}

// AdminCloseSessionRegistration performs early closure of an individual session.
func (s *Service) AdminCloseSessionRegistration(ctx context.Context, sessionID, actorID int64, req examreq.CloseRegistrationRequestDTO) (response.ExamSessionResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin close session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e ExamSessionEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'closed',
		    exam_session_registration_deadline = LEAST(COALESCE(exam_session_registration_deadline, NOW()), NOW()),
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE
		RETURNING exam_session_id, exam_session_exam_period_id, exam_session_exam_type_id,
		          exam_session_name, exam_session_description,
		          exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		          exam_session_registration_deadline, exam_session_status,
		          exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		          exam_session_duplicated_from_id`,
		sessionID,
	).Scan(
		&e.ExamSessionID, &e.ExamSessionExamPeriodID, &e.ExamSessionExamTypeID,
		&e.ExamSessionName, &e.ExamSessionDescription,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
		&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
		&e.ExamSessionDuplicatedFromID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamSessionResponseDTO{}, ErrExamSessionNotFound
	} else if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: close session registration: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"reason":     req.Reason,
		"status":     "closed",
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: commit close session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)
	return ToExamSessionResponseDTO(e), nil
}

// AdminUpdateExamSession updates an exam session's schedule, fee, shift, or capacity.
// Direct modification of schedule/fee is blocked if the session is published or already has active registrations.
func (s *Service) AdminUpdateExamSession(ctx context.Context, sessionID, actorID int64, req examreq.UpdateExamSessionRequestDTO) (response.ExamSessionResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin update session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e ExamSessionEntity
	err = tx.QueryRowContext(ctx, `
		SELECT exam_session_id, exam_session_exam_period_id, exam_session_exam_type_id,
		       exam_session_name, exam_session_description,
		       exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		       exam_session_registration_deadline, exam_session_status,
		       exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		       exam_session_duplicated_from_id
		FROM exam_sessions
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE
		FOR UPDATE`,
		sessionID,
	).Scan(
		&e.ExamSessionID, &e.ExamSessionExamPeriodID, &e.ExamSessionExamTypeID,
		&e.ExamSessionName, &e.ExamSessionDescription,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus,
		&e.ExamSessionPublicationStatus, &e.ExamSessionPublishAt, &e.ExamSessionCheckInAt,
		&e.ExamSessionDuplicatedFromID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamSessionResponseDTO{}, ErrExamSessionNotFound
	} else if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: lock session: %w", err)
	}

	if e.ExamSessionStatus == enum.SessionStatusCancelled {
		return response.ExamSessionResponseDTO{}, ErrExamSessionCancelled
	}

	if req.HasScheduleOrFeeChange() && e.ExamSessionPublicationStatus != enum.PublicationStatusDraft {
		var regCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM exam_registrations
			WHERE exam_registration_exam_session_id = $1 AND exam_registration_is_deleted = FALSE`,
			sessionID,
		).Scan(&regCount); err != nil {
			return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: count session registrations: %w", err)
		}
		if regCount > 0 || e.ExamSessionPublicationStatus == enum.PublicationStatusPublished {
			return response.ExamSessionResponseDTO{}, ErrSessionNotEditable
		}
	}

	sessionDate := e.ExamSessionDate
	if req.Date != nil {
		d, err := parseDate(*req.Date)
		if err != nil {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "định dạng ngày thi không hợp lệ (YYYY-MM-DD)"}
		}
		sessionDate = d
	}

	shift := e.ExamSessionShift
	if req.Shift != nil {
		sh := enum.ExamShift(*req.Shift)
		if !sh.Valid() {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "ca thi không hợp lệ (morning, afternoon, evening)"}
		}
		shift = sh
	}

	capacity := e.ExamSessionCapacity
	if req.Capacity != nil {
		if *req.Capacity <= 0 {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "sức chứa ca thi phải lớn hơn 0"}
		}
		capacity = req.Capacity
	}

	fee := e.ExamSessionFee
	if req.Fee != nil {
		if *req.Fee < 0 {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "lệ phí thi không được âm"}
		}
		fee = *req.Fee
	}

	deadline := e.ExamSessionRegistrationDeadline
	if req.RegistrationDeadline != nil {
		dl, err := parseOptionalRFC3339(*req.RegistrationDeadline)
		if err != nil {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "hạn đăng ký phải có định dạng RFC3339 hợp lệ"}
		}
		deadline = dl
	}

	checkInAt := e.ExamSessionCheckInAt
	if req.CheckInAt != nil {
		ci, err := parseOptionalRFC3339(*req.CheckInAt)
		if err != nil {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "thời gian check-in phải có định dạng RFC3339 hợp lệ"}
		}
		checkInAt = ci
	}

	var updated ExamSessionEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_date = $1,
		    exam_session_shift = $2,
		    exam_session_capacity = $3,
		    exam_session_fee = $4,
		    exam_session_registration_deadline = $5,
		    exam_session_check_in_at = $6,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $7
		RETURNING exam_session_id, exam_session_exam_period_id, exam_session_exam_type_id,
		          exam_session_name, exam_session_description,
		          exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		          exam_session_registration_deadline, exam_session_status,
		          exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		          exam_session_duplicated_from_id`,
		sessionDate, shift, capacity, fee, deadline, checkInAt, sessionID,
	).Scan(
		&updated.ExamSessionID, &updated.ExamSessionExamPeriodID, &updated.ExamSessionExamTypeID,
		&updated.ExamSessionName, &updated.ExamSessionDescription,
		&updated.ExamSessionDate, &updated.ExamSessionShift, &updated.ExamSessionCapacity, &updated.ExamSessionFee,
		&updated.ExamSessionRegistrationDeadline, &updated.ExamSessionStatus,
		&updated.ExamSessionPublicationStatus, &updated.ExamSessionPublishAt, &updated.ExamSessionCheckInAt,
		&updated.ExamSessionDuplicatedFromID,
	)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: update session: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"date":       updated.ExamSessionDate,
		"shift":      updated.ExamSessionShift,
		"capacity":   updated.ExamSessionCapacity,
		"fee":        updated.ExamSessionFee,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: commit update session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)
	return ToExamSessionResponseDTO(updated), nil
}

// AdminDuplicateExamSession duplicates an individual exam session within the same exam period.
func (s *Service) AdminDuplicateExamSession(ctx context.Context, sessionID, actorID int64, req examreq.DuplicateExamSessionRequestDTO) (response.ExamSessionResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin duplicate session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. Lock source session and verify it is not deleted
	var src ExamSessionEntity
	err = tx.QueryRowContext(ctx, `
		SELECT exam_session_id, exam_session_exam_period_id, exam_session_exam_type_id,
		       exam_session_exam_room_id, exam_session_name, exam_session_description,
		       exam_session_shift, exam_session_fee
		FROM exam_sessions
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE
		FOR SHARE`, sessionID,
	).Scan(
		&src.ExamSessionID, &src.ExamSessionExamPeriodID, &src.ExamSessionExamTypeID,
		&src.ExamSessionExamRoomID, &src.ExamSessionName, &src.ExamSessionDescription,
		&src.ExamSessionShift, &src.ExamSessionFee,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamSessionResponseDTO{}, ErrExamSessionNotFound
	} else if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: lock source session: %w", err)
	}

	// 2. Validate parent exam_period is not in a closed/final status
	if src.ExamSessionExamPeriodID != nil {
		var periodStatus string
		err = tx.QueryRowContext(ctx, `
			SELECT exam_period_status
			FROM exam_periods
			WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE`,
			*src.ExamSessionExamPeriodID,
		).Scan(&periodStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return response.ExamSessionResponseDTO{}, errors.New("Không tìm thấy đợt thi của ca thi")
		} else if err != nil {
			return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: check parent period status: %w", err)
		}

		if periodStatus == "registration_closed" || periodStatus == "cancelled" || periodStatus == "completed" {
			return response.ExamSessionResponseDTO{}, ErrParentPeriodClosed
		}
	}

	// 3. Name computation with chained duplication suffix handling
	var newName string
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		newName = strings.TrimSpace(*req.Name)
	} else {
		baseName := ""
		if src.ExamSessionName != nil && strings.TrimSpace(*src.ExamSessionName) != "" {
			baseName = strings.TrimSpace(*src.ExamSessionName)
		} else {
			var typeName string
			_ = tx.QueryRowContext(ctx, `SELECT exam_type_name FROM exam_types WHERE exam_type_id = $1`, src.ExamSessionExamTypeID).Scan(&typeName)
			baseName = fmt.Sprintf("%s - %s", typeName, strings.Title(string(src.ExamSessionShift)))
		}
		newName = formatDuplicateSessionName(baseName)
	}

	// 4. Insert new session with date, capacity, and deadlines set to NULL
	var created ExamSessionEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO exam_sessions (
			exam_session_exam_period_id,
			exam_session_exam_type_id,
			exam_session_exam_room_id,
			exam_session_name,
			exam_session_description,
			exam_session_date,
			exam_session_shift,
			exam_session_capacity,
			exam_session_fee,
			exam_session_registration_deadline,
			exam_session_status,
			exam_session_publication_status,
			exam_session_publish_at,
			exam_session_check_in_at,
			exam_session_duplicated_from_id
		) VALUES (
			$1, $2, $3, $4, $5, NULL, $6, NULL, $7, NULL, 'open', 'draft', NULL, NULL, $8
		)
		RETURNING exam_session_id, exam_session_exam_period_id, exam_session_exam_type_id,
		          exam_session_name, exam_session_description,
		          exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee,
		          exam_session_registration_deadline, exam_session_status,
		          exam_session_publication_status, exam_session_publish_at, exam_session_check_in_at,
		          exam_session_duplicated_from_id, exam_session_created_at, exam_session_updated_at`,
		src.ExamSessionExamPeriodID,
		src.ExamSessionExamTypeID,
		src.ExamSessionExamRoomID,
		newName,
		src.ExamSessionDescription,
		src.ExamSessionShift,
		src.ExamSessionFee,
		src.ExamSessionID,
	).Scan(
		&created.ExamSessionID, &created.ExamSessionExamPeriodID, &created.ExamSessionExamTypeID,
		&created.ExamSessionName, &created.ExamSessionDescription,
		&created.ExamSessionDate, &created.ExamSessionShift, &created.ExamSessionCapacity, &created.ExamSessionFee,
		&created.ExamSessionRegistrationDeadline, &created.ExamSessionStatus,
		&created.ExamSessionPublicationStatus, &created.ExamSessionPublishAt, &created.ExamSessionCheckInAt,
		&created.ExamSessionDuplicatedFromID, &created.ExamSessionCreatedAt, &created.ExamSessionUpdatedAt,
	)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: insert duplicated session: %w", err)
	}

	// 6. Audit logging
	auditPayload, _ := json.Marshal(map[string]any{
		"source_session_id": src.ExamSessionID,
		"new_session_id":    created.ExamSessionID,
		"period_id":         src.ExamSessionExamPeriodID,
		"exam_type_id":      src.ExamSessionExamTypeID,
		"name":              newName,
		"fee":               created.ExamSessionFee,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionDuplicate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       created.ExamSessionID,
		NewValue:       auditPayload,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: commit duplicate session tx: %w", err)
	}

	_ = s.ClearCache(ctx)
	return ToExamSessionResponseDTO(created), nil
}

func parseDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("định dạng ngày không hợp lệ (YYYY-MM-DD)")
	}
	return &t, nil
}

func truncateRunes(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 0 {
		return ""
	}
	return string(r[:maxLen])
}
