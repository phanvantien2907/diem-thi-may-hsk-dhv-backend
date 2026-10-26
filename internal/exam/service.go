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

var sessionCopySuffixRegex = regexp.MustCompile(`(?i)\s*\((copy|bản sao)\)+$`)

func formatDuplicateSessionName(name string) string {
	cleaned := sessionCopySuffixRegex.ReplaceAllString(strings.TrimSpace(name), "")
	return cleaned + duplicateNameSuffix
}

// Service handles exam types, rooms, sessions, and seat generation.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	cacheSvc *cache.Service
}

// sessionColumns is the canonical column list used by every exam_sessions read.
// The table must be aliased as `es`.
const sessionColumns = `es.exam_session_id, es.exam_session_exam_period_id, es.exam_session_exam_type_id,
	es.exam_session_name, es.exam_session_description,
	es.exam_session_date, es.exam_session_shift, es.exam_session_capacity, es.exam_session_fee,
	es.exam_session_registration_deadline, es.exam_session_status, es.exam_session_closed_reason,
	es.exam_session_publish_at, es.exam_session_check_in_at, es.exam_session_duplicated_from_id`

// sessionParentColumns carries the parent-period facts required to derive the
// effective session status. The parent table must be aliased as `ep`.
const sessionParentColumns = `ep.exam_period_status, ep.exam_period_registration_start,
	ep.exam_period_registration_deadline, ep.exam_period_end_date`

// sessionHeadCountExpr counts registrations that still hold a slot.
const sessionHeadCountExpr = `(
	SELECT COUNT(*) FROM exam_registrations r
	WHERE r.exam_registration_exam_session_id = es.exam_session_id
	  AND r.exam_registration_is_deleted = FALSE
	  AND r.exam_registration_status <> 'cancelled')`

const sessionFrom = ` FROM exam_sessions es
	LEFT JOIN exam_periods ep ON ep.exam_period_id = es.exam_session_exam_period_id`

const sessionSelect = `SELECT ` + sessionColumns + `, ` + sessionParentColumns + `, ` + sessionHeadCountExpr + sessionFrom

// periodColumns is the canonical column list used by every exam_periods read.
// The table must be aliased as `ep`.
const periodColumns = `ep.exam_period_id, ep.exam_period_name, ep.exam_period_code,
	ep.exam_period_description, ep.exam_period_instructions,
	ep.exam_period_start_date, ep.exam_period_end_date,
	ep.exam_period_registration_start, ep.exam_period_registration_deadline,
	ep.exam_period_status, ep.exam_period_original_date, ep.exam_period_reschedule_count,
	ep.exam_period_created_at, ep.exam_period_updated_at`

const periodMetrics = `,
	(SELECT COUNT(*) FROM exam_sessions x
	   WHERE x.exam_session_exam_period_id = ep.exam_period_id
	     AND x.exam_session_is_deleted = FALSE) AS session_count,
	(SELECT COUNT(*) FROM exam_sessions x
	   WHERE x.exam_session_exam_period_id = ep.exam_period_id
	     AND x.exam_session_is_deleted = FALSE
	     AND x.exam_session_status NOT IN ('draft', 'cancelled')
	     AND x.exam_session_date IS NOT NULL
	     AND x.exam_session_capacity IS NOT NULL
	     AND x.exam_session_registration_deadline IS NOT NULL) AS valid_session_count,
	(SELECT COUNT(*) FROM exam_registrations r
	   JOIN exam_sessions x ON x.exam_session_id = r.exam_registration_exam_session_id
	   WHERE x.exam_session_exam_period_id = ep.exam_period_id
	     AND r.exam_registration_is_deleted = FALSE
	     AND r.exam_registration_status <> 'cancelled') AS registration_count`

const periodSelect = `SELECT ` + periodColumns + periodMetrics + ` FROM exam_periods ep`

// publicSessionFilter keeps only sessions visible to candidates. A session whose
// scheduled publication time has elapsed counts as public even before the background
// sync rewrites the stored column.
const publicSessionFilter = `(es.exam_session_status IN ('open', 'closed', 'completed')
	OR (es.exam_session_status = 'scheduled' AND es.exam_session_publish_at IS NOT NULL
	    AND es.exam_session_publish_at <= NOW()))
	AND (ep.exam_period_status IS NULL OR ep.exam_period_status NOT IN ('draft', 'cancelled'))`

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func sessionScanDest(e *ExamSessionEntity, reason *sql.NullString) []any {
	return []any{
		&e.ExamSessionID, &e.ExamSessionExamPeriodID, &e.ExamSessionExamTypeID,
		&e.ExamSessionName, &e.ExamSessionDescription,
		&e.ExamSessionDate, &e.ExamSessionShift, &e.ExamSessionCapacity, &e.ExamSessionFee,
		&e.ExamSessionRegistrationDeadline, &e.ExamSessionStatus, reason,
		&e.ExamSessionPublishAt, &e.ExamSessionCheckInAt, &e.ExamSessionDuplicatedFromID,
	}
}

// scanSession reads one session row without parent-period context.
func scanSession(sc rowScanner, e *ExamSessionEntity) error {
	var reason sql.NullString
	if err := sc.Scan(sessionScanDest(e, &reason)...); err != nil {
		return err
	}
	e.ExamSessionClosedReason = enum.ExamSessionClosedReason(reason.String)
	return nil
}

// scanSessionWithParent reads one session row together with the parent period facts
// and the head count, then derives the guard state.
func scanSessionWithParent(sc rowScanner, e *ExamSessionEntity, st *SessionState) error {
	var (
		reason    sql.NullString
		periodSt  sql.NullString
		pRegStart sql.NullTime
		pRegDead  sql.NullTime
		pEndDate  sql.NullTime
		headCount int
	)
	dest := append(sessionScanDest(e, &reason), &periodSt, &pRegStart, &pRegDead, &pEndDate, &headCount)
	if err := sc.Scan(dest...); err != nil {
		return err
	}
	e.ExamSessionClosedReason = enum.ExamSessionClosedReason(reason.String)

	st.StoredStatus = e.ExamSessionStatus
	st.ClosedReason = e.ExamSessionClosedReason
	st.PublishAt = e.ExamSessionPublishAt
	st.Date = e.ExamSessionDate
	st.Deadline = e.ExamSessionRegistrationDeadline
	st.Capacity = e.ExamSessionCapacity
	st.RegisteredCount = headCount
	if periodSt.Valid {
		st.PeriodStatus = enum.ExamPeriodStatus(periodSt.String)
	}
	if pRegStart.Valid {
		t := pRegStart.Time
		st.PeriodRegStart = &t
	}
	if pRegDead.Valid {
		t := pRegDead.Time
		st.PeriodRegDeadline = &t
	}
	if pEndDate.Valid {
		t := pEndDate.Time
		st.PeriodEndDate = &t
	}
	return nil
}

func periodScanDest(e *ExamPeriodEntity) []any {
	return []any{
		&e.ExamPeriodID, &e.ExamPeriodName, &e.ExamPeriodCode,
		&e.ExamPeriodDescription, &e.ExamPeriodInstructions,
		&e.ExamPeriodStartDate, &e.ExamPeriodEndDate,
		&e.ExamPeriodRegistrationStart, &e.ExamPeriodRegistrationDeadline,
		&e.ExamPeriodStatus, &e.ExamPeriodOriginalDate, &e.ExamPeriodRescheduleCount,
		&e.ExamPeriodCreatedAt, &e.ExamPeriodUpdatedAt,
	}
}

// scanPeriodWithMetrics reads one period row plus session/registration counts and
// derives the guard state.
func scanPeriodWithMetrics(sc rowScanner, e *ExamPeriodEntity, st *PeriodState) (sessionCount int, err error) {
	var validSessions, registrations int64
	dest := append(periodScanDest(e), &sessionCount, &validSessions, &registrations)
	if err := sc.Scan(dest...); err != nil {
		return 0, err
	}
	*st = newPeriodState(*e)
	st.ValidSessionCount = validSessions
	st.RegistrationCount = registrations
	return sessionCount, nil
}

var (
	ErrExamSessionNotFound      = errors.New("Không tìm thấy ca thi")
	ErrExamPeriodNotFound       = errors.New("Không tìm thấy kỳ thi")
	ErrNonDraftDateModification = errors.New("Đợt thi đã mở đăng ký hoặc đã đóng đăng ký. Không thể thay đổi trực tiếp ngày thi hoặc hạn đăng ký; vui lòng sử dụng quy trình dời lịch (Reschedule).")
	// ErrExamPeriodCodeExists is returned when the requested period code collides with an existing one.
	ErrExamPeriodCodeExists = errors.New("Mã kỳ thi đã tồn tại. Vui lòng chọn mã khác.")
	// ErrSessionScheduleIncomplete blocks publishing a session whose date/deadline/capacity is still NULL.
	ErrSessionScheduleIncomplete = errors.New("Ca thi chưa có ngày thi, số lượng chỗ hoặc hạn đăng ký. Vui lòng cập nhật lịch thi trước khi công khai.")
	// ErrSessionNotEditable blocks direct schedule edits once a session has registrations.
	ErrSessionNotEditable = errors.New("Ca thi đã có thí sinh đăng ký. Không thể sửa trực tiếp ngày thi, ca thi, lệ phí hoặc hạn đăng ký; vui lòng sử dụng quy trình dời lịch (Reschedule).")
	// ErrParentPeriodClosed blocks duplicating an exam session into a closed/cancelled/completed period.
	ErrParentPeriodClosed = errors.New("Đợt thi đã đóng, đã hủy hoặc đã hoàn thành; không thể nhân bản thêm ca thi vào đợt này")
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

// lockSession loads a session for update together with its guard state.
func (s *Service) lockSession(ctx context.Context, tx *sql.Tx, sessionID int64) (ExamSessionEntity, SessionState, error) {
	var (
		e  ExamSessionEntity
		st SessionState
	)
	err := scanSessionWithParent(tx.QueryRowContext(ctx,
		sessionSelect+`
		WHERE es.exam_session_id = $1 AND es.exam_session_is_deleted = FALSE
		FOR UPDATE OF es`, sessionID), &e, &st)
	if errors.Is(err, sql.ErrNoRows) {
		return e, st, ErrExamSessionNotFound
	} else if err != nil {
		return e, st, fmt.Errorf("exam: lock session: %w", err)
	}
	return e, st, nil
}

// lockPeriod loads a period for update together with its guard state.
func (s *Service) lockPeriod(ctx context.Context, tx *sql.Tx, periodID int64) (ExamPeriodEntity, PeriodState, error) {
	var (
		e  ExamPeriodEntity
		st PeriodState
	)
	_, err := scanPeriodWithMetrics(tx.QueryRowContext(ctx,
		periodSelect+`
		WHERE ep.exam_period_id = $1 AND ep.exam_period_is_deleted = FALSE
		FOR UPDATE OF ep`, periodID), &e, &st)
	if errors.Is(err, sql.ErrNoRows) {
		return e, st, ErrExamPeriodNotFound
	} else if err != nil {
		return e, st, fmt.Errorf("exam: lock period: %w", err)
	}
	return e, st, nil
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

// ListExamSessions returns every publicly visible session.
func (s *Service) ListExamSessions(ctx context.Context) ([]response.ExamSessionResponseDTO, error) {
	if s.cacheSvc != nil {
		var cached []response.ExamSessionResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKeyExamSessionsPub, &cached); hit {
			return cached, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, sessionSelect+`
		WHERE es.exam_session_is_deleted = FALSE AND `+publicSessionFilter+`
		ORDER BY es.exam_session_date ASC`)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	list := make([]response.ExamSessionResponseDTO, 0)
	for rows.Next() {
		var (
			e  ExamSessionEntity
			st SessionState
		)
		if err := scanSessionWithParent(rows, &e, &st); err != nil {
			return nil, fmt.Errorf("exam: scan session: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e, st, now))
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

// GetExamSession returns a single publicly visible session DTO by ID.
func (s *Service) GetExamSession(ctx context.Context, id int64) (response.ExamSessionResponseDTO, error) {
	cacheKey := fmt.Sprintf("%s%d", cachePrefixExamSession, id)
	if s.cacheSvc != nil {
		var cached response.ExamSessionResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	var (
		e  ExamSessionEntity
		st SessionState
	)
	err := scanSessionWithParent(s.db.QueryRowContext(ctx, sessionSelect+`
		WHERE es.exam_session_id = $1 AND es.exam_session_is_deleted = FALSE AND `+publicSessionFilter, id), &e, &st)
	if errors.Is(err, sql.ErrNoRows) {
		if s.cacheSvc != nil {
			s.cacheSvc.SetSentinel(ctx, cacheKey, 1*time.Minute)
		}
		return response.ExamSessionResponseDTO{}, ErrExamSessionNotFound
	}
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: get session: %w", err)
	}
	dto := ToExamSessionResponseDTO(e, st, time.Now())
	if s.cacheSvc != nil {
		// Apply ±35% dynamic jitter to prevent simultaneous expiry
		_ = s.cacheSvc.SetWithJitter(ctx, cacheKey, dto, 10*time.Minute, 0.35)
	}
	return dto, nil
}

// ListExamSeats returns seats for a publicly visible session.
func (s *Service) ListExamSeats(ctx context.Context, sessionID int64) ([]response.ExamSeatResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT es.exam_seat_id, es.exam_seat_exam_session_id, es.exam_seat_number,
		       es.exam_seat_status, es.exam_seat_held_until
		FROM exam_seats es
		JOIN exam_sessions sess ON sess.exam_session_id = es.exam_seat_exam_session_id
		LEFT JOIN exam_periods ep ON ep.exam_period_id = sess.exam_session_exam_period_id
		WHERE es.exam_seat_exam_session_id = $1
		  AND es.exam_seat_is_deleted = FALSE
		  AND sess.exam_session_is_deleted = FALSE
		  AND ((sess.exam_session_status IN ('open', 'closed', 'completed'))
		       OR (sess.exam_session_status = 'scheduled' AND sess.exam_session_publish_at IS NOT NULL
		           AND sess.exam_session_publish_at <= NOW()))
		  AND (ep.exam_period_status IS NULL OR ep.exam_period_status NOT IN ('draft', 'cancelled'))
		ORDER BY es.exam_seat_number ASC`, sessionID)
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
		return 0, ErrExamSessionNotFound
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

// ListExamSessionsFiltered returns publicly visible sessions matching optional filters.
func (s *Service) ListExamSessionsFiltered(ctx context.Context, f examreq.ListExamSessionsFilterDTO) ([]response.ExamSessionResponseDTO, error) {
	return s.listExamSessionsFiltered(ctx, f, true)
}

// ListAdminExamSessionsFiltered returns every non-deleted session for administrators.
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

	args := []any{}
	where := "WHERE es.exam_session_is_deleted = FALSE"
	if publicOnly {
		where += " AND " + publicSessionFilter
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

	rows, err := s.db.QueryContext(ctx, sessionSelect+" "+where+" ORDER BY es.exam_session_date ASC", args...)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions filtered: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	list := make([]response.ExamSessionResponseDTO, 0)
	for rows.Next() {
		var (
			e  ExamSessionEntity
			st SessionState
		)
		if err := scanSessionWithParent(rows, &e, &st); err != nil {
			return nil, fmt.Errorf("exam: scan session filtered: %w", err)
		}
		list = append(list, ToExamSessionResponseDTO(e, st, now))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.SetWithJitter(ctx, cacheKey, list, 10*time.Minute, 0.35)
	}
	return list, nil
}

// ListAvailablePeriodActions exposes the action matrix for a single period.
func (s *Service) ListAvailablePeriodActions(ctx context.Context, periodID int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return nil, err
	}
	return actionStrings(AvailablePeriodActions(st, time.Now())), nil
}

// CancelExamSession cancels a session. Only effectively open or closed sessions may be
// cancelled; drafts and scheduled sessions must be deleted instead.
func (s *Service) CancelExamSession(ctx context.Context, sessionID, actorID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return err
	}
	if err := GuardSessionAction(ActionCancel, st, time.Now()); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_sessions
		 SET exam_session_status = 'cancelled', exam_session_closed_reason = NULL,
		     exam_session_updated_at = NOW()
		 WHERE exam_session_id = $1`, sessionID,
	); err != nil {
		return fmt.Errorf("exam: cancel session: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": e.ExamSessionStatus.String()})
	newVal, _ := json.Marshal(map[string]any{"status": enum.SessionStatusCancelled.String()})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionDelete.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("exam: commit cancel session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)
	return nil
}

// PublishExamSession publishes a session immediately or schedules the publication when
// req.PublishAt is a future RFC3339 timestamp.
func (s *Service) PublishExamSession(ctx context.Context, sessionID, actorID int64, req examreq.PublishSessionRequestDTO) (response.UpdateSessionStatusResponseDTO, error) {
	publishAt, err := parseOptionalRFC3339(req.PublishAt)
	if err != nil {
		return response.UpdateSessionStatusResponseDTO{}, ValidationError{Msg: "publish_at phải có định dạng RFC3339 hợp lệ"}
	}
	now := time.Now()
	if publishAt != nil && !publishAt.After(now) {
		return response.UpdateSessionStatusResponseDTO{}, ValidationError{Msg: "Thời điểm công khai phải ở trong tương lai"}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: begin publish tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.UpdateSessionStatusResponseDTO{}, err
	}

	action := ActionPublish
	if publishAt != nil {
		action = ActionSchedulePublish
	}
	if err := GuardSessionAction(action, st, now); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, err
	}
	// A duplicated session starts with NULL date/deadline/capacity. It must never become
	// visible until the admin re-enters the schedule and slots.
	if e.ExamSessionDate == nil || e.ExamSessionCapacity == nil || *e.ExamSessionCapacity <= 0 || e.ExamSessionRegistrationDeadline == nil {
		return response.UpdateSessionStatusResponseDTO{}, ErrSessionScheduleIncomplete
	}
	if err := ensureParentPeriodPublishable(st.EffectiveParent(now)); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, err
	}

	newStatus := enum.SessionStatusOpen
	if publishAt != nil {
		newStatus = enum.SessionStatusScheduled
	}

	var updated ExamSessionEntity
	if err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = $1,
		    exam_session_closed_reason = NULL,
		    exam_session_publish_at = $2,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $3
		RETURNING `+sessionColumns, newStatus, publishAt, sessionID), &updated); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: publish session: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": e.ExamSessionStatus.String()})
	newVal, _ := json.Marshal(map[string]any{
		"status":     newStatus.String(),
		"publish_at": publishAt,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: commit publish session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = updated.ExamSessionClosedReason
	st.PublishAt = updated.ExamSessionPublishAt
	message := "Đã công khai ca thi"
	if publishAt != nil {
		message = "Đã lên lịch công khai ca thi"
	}
	return response.UpdateSessionStatusResponseDTO{
		Message:   message,
		PublishAt: publishAt,
		Session:   ToExamSessionResponseDTO(updated, st, now),
	}, nil
}

// UnpublishExamSession hides a session again (scheduled -> cancelled schedule,
// open with no registrations -> draft).
func (s *Service) UnpublishExamSession(ctx context.Context, sessionID, actorID int64) (response.UpdateSessionStatusResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: begin unpublish tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.UpdateSessionStatusResponseDTO{}, err
	}
	if err := GuardSessionAction(ActionUnpublish, st, now); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, err
	}

	var updated ExamSessionEntity
	if err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'draft',
		    exam_session_closed_reason = NULL,
		    exam_session_publish_at = NULL,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $1
		RETURNING `+sessionColumns, sessionID), &updated); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: unpublish session: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": e.ExamSessionStatus.String()})
	newVal, _ := json.Marshal(map[string]any{"status": enum.SessionStatusDraft.String()})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.UpdateSessionStatusResponseDTO{}, fmt.Errorf("exam: commit unpublish session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = ""
	st.PublishAt = nil
	return response.UpdateSessionStatusResponseDTO{
		Message: "Đã thu hồi công khai ca thi",
		Session: ToExamSessionResponseDTO(updated, st, now),
	}, nil
}

// CloseSessionRegistration closes a session's registration early. The closed reason
// decides how the session can be reopened later.
func (s *Service) CloseSessionRegistration(ctx context.Context, sessionID, actorID int64, req examreq.CloseRegistrationRequestDTO) (response.ExamSessionResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin close session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if err := GuardSessionAction(ActionCloseRegistration, st, now); err != nil {
		return response.ExamSessionResponseDTO{}, err
	}

	reason := enum.ExamSessionClosedReason(req.ClosedReason)
	if reason == "" {
		reason = enum.ClosedReasonManual
	}
	if !reason.Valid() {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: "Lý do đóng không hợp lệ (manual, full, deadline, period_closed)"}
	}

	var updated ExamSessionEntity
	if err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'closed',
		    exam_session_closed_reason = $1,
		    exam_session_registration_deadline = LEAST(COALESCE(exam_session_registration_deadline, NOW()), NOW()),
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $2
		RETURNING `+sessionColumns, reason, sessionID), &updated); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: close session registration: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"session_id":    sessionID,
		"reason":        req.Reason,
		"status":        enum.SessionStatusClosed.String(),
		"closed_reason": reason.String(),
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

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = updated.ExamSessionClosedReason
	st.Deadline = updated.ExamSessionRegistrationDeadline
	_ = e
	return ToExamSessionResponseDTO(updated, st, now), nil
}

// ReopenSessionRegistration reopens a closed session when the parent period is open for
// registration. A new deadline is mandatory when the session closed because its deadline
// elapsed; a full session must first have its capacity increased.
func (s *Service) ReopenSessionRegistration(ctx context.Context, sessionID, actorID int64, req examreq.ReopenSessionRegistrationRequestDTO) (response.ExamSessionResponseDTO, error) {
	now := time.Now()
	newDeadline, err := parseOptionalRFC3339(req.RegistrationDeadline)
	if err != nil {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: "hạn đăng ký phải có định dạng RFC3339 hợp lệ"}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin reopen session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if err := GuardSessionAction(ActionReopenRegistration, st, now); err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if st.ClosedReason == enum.ClosedReasonDeadline {
		if newDeadline == nil || !newDeadline.After(now) {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "Ca thi đóng do hết hạn; phải đặt hạn đăng ký mới trong tương lai"}
		}
	}
	if newDeadline != nil && !newDeadline.After(now) {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: "Hạn đăng ký mới phải ở trong tương lai"}
	}

	deadline := e.ExamSessionRegistrationDeadline
	if newDeadline != nil {
		deadline = newDeadline
	}

	var updated ExamSessionEntity
	if err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'open',
		    exam_session_closed_reason = NULL,
		    exam_session_registration_deadline = $1,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $2
		RETURNING `+sessionColumns, deadline, sessionID), &updated); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: reopen session registration: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{
		"status":        enum.SessionStatusClosed.String(),
		"closed_reason": st.ClosedReason.String(),
	})
	newVal, _ := json.Marshal(map[string]any{
		"status":                enum.SessionStatusOpen.String(),
		"registration_deadline": deadline,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: commit reopen session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = ""
	st.Deadline = updated.ExamSessionRegistrationDeadline
	return ToExamSessionResponseDTO(updated, st, now), nil
}

// UpdateSessionCapacity raises or lowers a session's seat capacity. Raising the capacity of
// a full session reopens it automatically; lowering below the number of registrations is
// rejected.
func (s *Service) UpdateSessionCapacity(ctx context.Context, sessionID, actorID int64, req examreq.UpdateCapacityRequestDTO) (response.ExamSessionResponseDTO, error) {
	now := time.Now()
	if req.Capacity <= 0 {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: "Sức chứa ca thi phải lớn hơn 0"}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin capacity tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.ExamSessionResponseDTO{}, err
	}

	current := 0
	if e.ExamSessionCapacity != nil {
		current = *e.ExamSessionCapacity
	}
	action := ActionIncreaseCapacity
	if req.Capacity < current {
		action = ActionDecreaseCapacity
	}
	if err := GuardSessionAction(action, st, now); err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if req.Capacity < st.RegisteredCount {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: fmt.Sprintf(
			"Không thể giảm sĩ số xuống %d vì đã có %d thí sinh đăng ký", req.Capacity, st.RegisteredCount)}
	}

	newStatus := e.ExamSessionStatus
	newReason := e.ExamSessionClosedReason
	// Increasing the capacity of a session that closed because it was full reopens it.
	if req.Capacity > current && st.ClosedReason == enum.ClosedReasonFull && st.RegisteredCount < req.Capacity {
		if st.EffectiveParent(now) == "" || st.EffectiveParent(now) == enum.PeriodStatusRegistrationOpen {
			newStatus = enum.SessionStatusOpen
			newReason = ""
		}
	}

	var updated ExamSessionEntity
	if err := scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_capacity = $1,
		    exam_session_status = $2,
		    exam_session_closed_reason = $3,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $4
		RETURNING `+sessionColumns,
		req.Capacity, newStatus, nullableReason(newReason), sessionID), &updated); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: update session capacity: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"capacity": current})
	newVal, _ := json.Marshal(map[string]any{"capacity": req.Capacity, "status": newStatus.String()})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: commit session capacity: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = updated.ExamSessionClosedReason
	st.Capacity = updated.ExamSessionCapacity
	return ToExamSessionResponseDTO(updated, st, now), nil
}

// DeleteExamSessionDraft soft-deletes a draft or scheduled session.
func (s *Service) DeleteExamSessionDraft(ctx context.Context, sessionID, actorID int64) error {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("exam: begin delete session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return err
	}
	if err := GuardSessionAction(ActionDeleteDraft, st, now); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_is_deleted = TRUE, exam_session_deleted_at = NOW(), exam_session_updated_at = NOW()
		WHERE exam_session_id = $1 AND exam_session_is_deleted = FALSE`, sessionID); err != nil {
		return fmt.Errorf("exam: delete draft session: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"session_id": sessionID, "status": e.ExamSessionStatus.String()})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionSoftDelete.String(),
		EntityTable:    auditenum.AuditEntityExamSession.String(),
		EntityID:       sessionID,
		OldValue:       oldVal,
	})

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("exam: commit delete draft session: %w", err)
	}
	s.invalidateSessionCache(ctx, sessionID)
	return nil
}

// SyncStoredStatuses keeps the persisted status columns aligned with the time-derived
// effective status. The effective status is always computed on read, so this sync only
// exists so lists, filters, and reports can be sorted by the stored column.
func (s *Service) SyncStoredStatuses(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("exam: begin status sync tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var affected int64

	// 1. Periods whose registration window opened or elapsed.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'registration_open', exam_period_updated_at = NOW()
		WHERE exam_period_is_deleted = FALSE
		  AND exam_period_status = 'published'
		  AND exam_period_registration_deadline IS NOT NULL
		  AND exam_period_registration_deadline > NOW()
		  AND (exam_period_registration_start IS NULL OR exam_period_registration_start <= NOW())`); err != nil {
		return 0, fmt.Errorf("exam: sync periods to open: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'registration_closed', exam_period_updated_at = NOW()
		WHERE exam_period_is_deleted = FALSE
		  AND exam_period_status IN ('published', 'registration_open')
		  AND exam_period_registration_deadline IS NOT NULL
		  AND exam_period_registration_deadline <= NOW()`); err != nil {
		return 0, fmt.Errorf("exam: sync periods to closed: %w", err)
	}

	// 2. Scheduled sessions whose publication time arrived.
	res, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'open',
		    exam_session_publish_at = NULL,
		    exam_session_updated_at = NOW()
		WHERE exam_session_is_deleted = FALSE
		  AND exam_session_status = 'scheduled'
		  AND exam_session_publish_at IS NOT NULL
		  AND exam_session_publish_at <= NOW()
		  AND exam_session_date IS NOT NULL
		  AND exam_session_capacity IS NOT NULL
		  AND exam_session_registration_deadline IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM exam_periods ep
		      WHERE ep.exam_period_id = exam_session_exam_period_id
		        AND ep.exam_period_is_deleted = FALSE
		        AND ep.exam_period_status <> 'registration_open')`)
	if err != nil {
		return 0, fmt.Errorf("exam: sync scheduled sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	affected += n

	// 3. Sessions follow their period into `closed` once the period stops accepting
	//    registration. Only `registration_closed` cascades: a `draft` or `published`
	//    period must NOT close its children, otherwise reopening registration would
	//    leave them permanently `closed` with reason `period_closed`.
	res, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions es
		SET exam_session_status = 'closed',
		    exam_session_closed_reason = 'period_closed',
		    exam_session_updated_at = NOW()
		FROM exam_periods ep
		WHERE ep.exam_period_id = es.exam_session_exam_period_id
		  AND es.exam_session_is_deleted = FALSE
		  AND es.exam_session_status = 'open'
		  AND ep.exam_period_is_deleted = FALSE
		  AND ep.exam_period_status = 'registration_closed'`)
	if err != nil {
		return 0, fmt.Errorf("exam: sync sessions to period_closed: %w", err)
	}
	n, _ = res.RowsAffected()
	affected += n

	// 3b. Scheduled children of a closed period must not go live: cancel the schedule.
	res, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions es
		SET exam_session_status = 'draft',
		    exam_session_publish_at = NULL,
		    exam_session_updated_at = NOW()
		FROM exam_periods ep
		WHERE ep.exam_period_id = es.exam_session_exam_period_id
		  AND es.exam_session_is_deleted = FALSE
		  AND es.exam_session_status = 'scheduled'
		  AND ep.exam_period_is_deleted = FALSE
		  AND ep.exam_period_status = 'registration_closed'`)
	if err != nil {
		return 0, fmt.Errorf("exam: cancel scheduled sessions of closed periods: %w", err)
	}
	n, _ = res.RowsAffected()
	affected += n

	// 4. Sessions whose own registration deadline elapsed.
	res, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'closed',
		    exam_session_closed_reason = 'deadline',
		    exam_session_updated_at = NOW()
		WHERE exam_session_is_deleted = FALSE
		  AND exam_session_status = 'open'
		  AND exam_session_registration_deadline IS NOT NULL
		  AND exam_session_registration_deadline <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("exam: sync sessions to deadline: %w", err)
	}
	n, _ = res.RowsAffected()
	affected += n

	// 5. Sessions whose capacity is exhausted.
	res, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions es
		SET exam_session_status = 'closed',
		    exam_session_closed_reason = 'full',
		    exam_session_updated_at = NOW()
		WHERE es.exam_session_is_deleted = FALSE
		  AND es.exam_session_status = 'open'
		  AND es.exam_session_capacity IS NOT NULL
		  AND es.exam_session_capacity > 0
		  AND es.exam_session_capacity <= (
		      SELECT COUNT(*) FROM exam_registrations r
		      WHERE r.exam_registration_exam_session_id = es.exam_session_id
		        AND r.exam_registration_is_deleted = FALSE
		        AND r.exam_registration_status <> 'cancelled')`)
	if err != nil {
		return 0, fmt.Errorf("exam: sync sessions to full: %w", err)
	}
	n, _ = res.RowsAffected()
	affected += n

	// 6. Sessions whose exam day has passed.
	res, err = tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'completed',
		    exam_session_closed_reason = NULL,
		    exam_session_updated_at = NOW()
		WHERE exam_session_is_deleted = FALSE
		  AND exam_session_status IN ('open', 'closed')
		  AND exam_session_date IS NOT NULL
		  AND exam_session_date < CURRENT_DATE`)
	if err != nil {
		return 0, fmt.Errorf("exam: sync sessions to completed: %w", err)
	}
	n, _ = res.RowsAffected()
	affected += n

	if affected > 0 {
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			Action:      auditenum.AuditActionUpdate.String(),
			EntityTable: auditenum.AuditEntityExamSession.String(),
			EntityID:    0,
			NewValue:    json.RawMessage(`{"job":"sync_stored_statuses"}`),
		})
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("exam: commit status sync: %w", err)
	}
	if affected > 0 {
		_ = s.ClearCache(ctx)
	}
	return affected, nil
}

// BatchCreateExamSessions creates multiple sessions in a single transaction.
func (s *Service) BatchCreateExamSessions(ctx context.Context, reqs []examreq.CreateExamSessionRequestDTO, actorID int64) ([]response.ExamSessionResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("exam: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now()
	list := make([]response.ExamSessionResponseDTO, 0, len(reqs))
	for _, r := range reqs {
		deadline, err := time.Parse(time.RFC3339, r.RegistrationDeadline)
		if err != nil {
			return nil, ValidationError{Msg: "hạn đăng ký phải có định dạng RFC3339 hợp lệ"}
		}
		status := enum.SessionStatusOpen
		if r.Status != "" {
			status = enum.ExamSessionStatus(r.Status)
		}
		if !status.Valid() || status == enum.SessionStatusClosed ||
			status == enum.SessionStatusCompleted || status == enum.SessionStatusCancelled {
			return nil, ValidationError{Msg: "Trạng thái ca thi không hợp lệ (open, draft, scheduled)"}
		}
		publishAt, err := parseOptionalRFC3339(r.PublishAt)
		if err != nil {
			return nil, ValidationError{Msg: "publish_at phải có định dạng RFC3339 hợp lệ"}
		}
		checkInAt, err := parseOptionalRFC3339(r.CheckInAt)
		if err != nil {
			return nil, ValidationError{Msg: "check_in_at phải có định dạng RFC3339 hợp lệ"}
		}
		switch status {
		case enum.SessionStatusScheduled:
			if publishAt == nil || !publishAt.After(now) {
				return nil, ValidationError{Msg: "Ca thi hẹn giờ phải có publish_at trong tương lai"}
			}
		default:
			publishAt = nil
		}

		var e ExamSessionEntity
		if err := scanSession(tx.QueryRowContext(ctx, `
			INSERT INTO exam_sessions (
				exam_session_exam_type_id,
				exam_session_name,
				exam_session_date, exam_session_shift, exam_session_capacity,
				exam_session_fee, exam_session_registration_deadline, exam_session_status,
				exam_session_publish_at, exam_session_check_in_at
			) VALUES (
				$1,
				(SELECT exam_type_name FROM exam_types WHERE exam_type_id = $1) || ' - ' || INITCAP($3),
				$2::DATE,$3,$4,$5,$6,$7,$8,$9
			)
			RETURNING `+sessionColumns,
			r.ExamTypeID, r.Date, r.Shift, r.Capacity, r.Fee, deadline, status,
			publishAt, checkInAt,
		), &e); err != nil {
			return nil, fmt.Errorf("exam: batch create session: %w", err)
		}
		var headCount int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM exam_registrations WHERE exam_registration_exam_session_id = $1`,
			e.ExamSessionID).Scan(&headCount); err != nil {
			return nil, fmt.Errorf("exam: count session registrations: %w", err)
		}
		st := newSessionState(e, "", nil, nil, nil, headCount)
		list = append(list, ToExamSessionResponseDTO(e, st, now))
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

// AdminListExamPeriods lists exam periods with session counts and filtering.
func (s *Service) AdminListExamPeriods(ctx context.Context, f examreq.ListExamPeriodsFilterDTO) ([]response.ExamPeriodResponseDTO, error) {
	where := "WHERE ep.exam_period_is_deleted = FALSE"
	args := []any{}
	n := 1

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
	_ = n

	rows, err := s.db.QueryContext(ctx, periodSelect+" "+where+`
		ORDER BY ep.exam_period_start_date DESC NULLS LAST, ep.exam_period_id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: list periods: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	list := make([]response.ExamPeriodResponseDTO, 0)
	for rows.Next() {
		var (
			e  ExamPeriodEntity
			st PeriodState
		)
		sessionCount, err := scanPeriodWithMetrics(rows, &e, &st)
		if err != nil {
			return nil, fmt.Errorf("exam: scan period: %w", err)
		}
		dto := ToExamPeriodResponseDTO(e, st, sessionCount, nil, now)
		// Filtering happens after deriving the effective status so a stale stored column
		// never hides a period from the requested bucket.
		if f.Status != "" && f.Status != "all" && dto.Status != f.Status {
			continue
		}
		list = append(list, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// AdminGetExamPeriod returns a single period with all child sessions.
func (s *Service) AdminGetExamPeriod(ctx context.Context, periodID int64) (response.ExamPeriodResponseDTO, error) {
	var (
		e  ExamPeriodEntity
		st PeriodState
	)
	sessionCount, err := scanPeriodWithMetrics(s.db.QueryRowContext(ctx, periodSelect+`
		WHERE ep.exam_period_id = $1 AND ep.exam_period_is_deleted = FALSE`, periodID), &e, &st)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamPeriodResponseDTO{}, ErrExamPeriodNotFound
	} else if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: get period: %w", err)
	}

	sessions, err := s.listPeriodSessions(ctx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	return ToExamPeriodResponseDTO(e, st, sessionCount, sessions, time.Now()), nil
}

// listPeriodSessions returns every child session of a period with derived state.
func (s *Service) listPeriodSessions(ctx context.Context, periodID int64) ([]response.ExamSessionResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, sessionSelect+`
		WHERE es.exam_session_exam_period_id = $1 AND es.exam_session_is_deleted = FALSE
		ORDER BY es.exam_session_date ASC NULLS LAST, es.exam_session_id ASC`, periodID)
	if err != nil {
		return nil, fmt.Errorf("exam: get period sessions: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	sessions := make([]response.ExamSessionResponseDTO, 0)
	for rows.Next() {
		var (
			se ExamSessionEntity
			st SessionState
		)
		if err := scanSessionWithParent(rows, &se, &st); err != nil {
			return nil, fmt.Errorf("exam: scan period session: %w", err)
		}
		sessions = append(sessions, ToExamSessionResponseDTO(se, st, now))
	}
	return sessions, rows.Err()
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
		RETURNING `+periodColumns,
		req.Name, req.Code, desc, inst, startDate, endDate, regStart, regDeadline,
	).Scan(periodScanDest(&e)...)
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
	return ToExamPeriodResponseDTO(e, newPeriodState(e), 0, nil, time.Now()), nil
}

// AdminDuplicateExamPeriod clones an exam period's configuration in ONE atomic transaction.
func (s *Service) AdminDuplicateExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.DuplicateExamPeriodRequestDTO) (response.DuplicateExamPeriodResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: begin duplicate tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

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
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: load source period: %w", err)
	}

	newName := req.Name
	if newName == "" {
		newName = src.ExamPeriodName + duplicateNameSuffix
	}
	newCode := req.Code
	if newCode == "" {
		newCode = src.ExamPeriodCode + "-COPY"
	}

	var newPeriod ExamPeriodEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO exam_periods (
			exam_period_name, exam_period_code, exam_period_description, exam_period_instructions,
			exam_period_status
		) VALUES ($1, $2, $3, $4, 'draft')
		RETURNING `+periodColumns,
		newName, newCode, src.ExamPeriodDescription, src.ExamPeriodInstructions,
	).Scan(periodScanDest(&newPeriod)...)
	if err != nil {
		if isUniqueViolation(err) {
			return response.DuplicateExamPeriodResponseDTO{}, ErrExamPeriodCodeExists
		}
		return response.DuplicateExamPeriodResponseDTO{}, fmt.Errorf("exam: insert duplicated period: %w", err)
	}

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
		       NULL, NULL, 'draft', NULL, NULL
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
		Period:             ToExamPeriodResponseDTO(newPeriod, newPeriodState(newPeriod), copiedSessions, nil, time.Now()),
	}, nil
}

// AdminUpdateExamPeriod updates exam period information.
// Draft and published periods accept every field; registration_open and
// registration_closed accept only Description and Instructions.
func (s *Service) AdminUpdateExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.UpdateExamPeriodRequestDTO) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin update tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionEdit, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if !req.HasAnyChange() {
		return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "Không có thông tin nào cần cập nhật"}
	}

	effective := st.Effective(now)
	scheduleEditable := effective == enum.PeriodStatusDraft || effective == enum.PeriodStatusPublished
	if req.HasScheduleChange() && !scheduleEditable {
		return response.ExamPeriodResponseDTO{}, ErrNonDraftDateModification
	}

	name := e.ExamPeriodName
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
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

	if scheduleEditable {
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
		RETURNING `+periodColumns,
		name, desc, inst, startDate, endDate, regStart, regDeadline, periodID,
	).Scan(periodScanDest(&updated)...)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: update period: %w", err)
	}

	// While the period is still a draft, propagate its dates to child sessions that have none.
	if effective == enum.PeriodStatusDraft && scheduleEditable && (startDate != nil || regDeadline != nil) {
		_, _ = tx.ExecContext(ctx, `
			UPDATE exam_sessions
			SET exam_session_date = COALESCE(exam_session_date, $1),
			    exam_session_registration_deadline = COALESCE(exam_session_registration_deadline, $2),
			    exam_session_updated_at = NOW()
			WHERE exam_session_exam_period_id = $3
			  AND exam_session_status = 'draft'
			  AND exam_session_is_deleted = FALSE`,
			startDate, regDeadline, periodID,
		)
	}

	newVal, _ := json.Marshal(map[string]any{"name": updated.ExamPeriodName, "status": updated.ExamPeriodStatus.String()})
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
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminPublishExamPeriod publishes a draft period so its sessions become visible.
func (s *Service) AdminPublishExamPeriod(ctx context.Context, periodID, actorID int64) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin publish period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionPublish, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'published', exam_period_updated_at = NOW()
		WHERE exam_period_id = $1
		RETURNING `+periodColumns, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: publish period: %w", err)
	}
	// Scheduled children may only go live once the period itself is public.
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       json.RawMessage(`{"status":"published"}`),
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit publish period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminUnpublishExamPeriod returns a published period to draft. Only periods without
// registrations may be unpublished.
func (s *Service) AdminUnpublishExamPeriod(ctx context.Context, periodID, actorID int64) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin unpublish period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionUnpublish, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'draft', exam_period_updated_at = NOW()
		WHERE exam_period_id = $1
		RETURNING `+periodColumns, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: unpublish period: %w", err)
	}
	// Scheduled sessions cannot go live inside a draft period: cancel their schedules.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'draft', exam_session_publish_at = NULL, exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status = 'scheduled'`, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: cancel scheduled sessions on unpublish: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       json.RawMessage(`{"status":"draft"}`),
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit unpublish period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminDeleteExamPeriod soft-deletes a draft period and its draft/scheduled sessions.
func (s *Service) AdminDeleteExamPeriod(ctx context.Context, periodID, actorID int64) error {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("exam: begin delete period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return err
	}
	if err := GuardPeriodAction(ActionDeleteDraft, st, now); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_is_deleted = TRUE, exam_session_deleted_at = NOW(), exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status IN ('draft', 'scheduled')`, periodID); err != nil {
		return fmt.Errorf("exam: delete draft period sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_periods
		SET exam_period_is_deleted = TRUE, exam_period_deleted_at = NOW(), exam_period_updated_at = NOW()
		WHERE exam_period_id = $1`, periodID); err != nil {
		return fmt.Errorf("exam: delete draft period: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionSoftDelete.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
	})

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("exam: commit delete period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return nil
}

// AdminCompleteExamPeriod marks a period and its remaining sessions as completed.
func (s *Service) AdminCompleteExamPeriod(ctx context.Context, periodID, actorID int64) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin complete period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionComplete, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'completed', exam_period_updated_at = NOW()
		WHERE exam_period_id = $1
		RETURNING `+periodColumns, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: complete period: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'completed', exam_session_closed_reason = NULL, exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status IN ('open', 'closed')`, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: complete period sessions: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       json.RawMessage(`{"status":"completed"}`),
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit complete period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminCancelExamPeriod cancels a period, cancels every child session, and resolves each
// registration through a refund or a transfer to another session.
func (s *Service) AdminCancelExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.CancelPeriodRequestDTO) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	resolution := strings.TrimSpace(req.Resolution)
	if resolution != "refund" && resolution != "transfer" {
		return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "Phải chọn hình thức xử lý đăng ký: hoàn tiền (refund) hoặc chuyển ca (transfer)"}
	}
	if resolution == "transfer" && req.TargetSession < 1 {
		return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "Chuyển ca yêu cầu target_session_id của ca thi đích"}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin cancel period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionCancel, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'cancelled', exam_period_updated_at = NOW()
		WHERE exam_period_id = $1
		RETURNING `+periodColumns, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: cancel period: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'cancelled', exam_session_closed_reason = NULL,
		    exam_session_publish_at = NULL, exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1 AND exam_session_is_deleted = FALSE`, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: cancel period sessions: %w", err)
	}

	moved, refunded, err := s.resolvePeriodRegistrations(ctx, tx, periodID, resolution, req.TargetSession)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	newVal, _ := json.Marshal(map[string]any{
		"status":               enum.PeriodStatusCancelled.String(),
		"reason":               req.Reason,
		"resolution":           resolution,
		"refunded_payments":    refunded,
		"transferred_sessions": moved,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit cancel period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// resolvePeriodRegistrations cancels every registration of a period's sessions and either
// refunds the successful payment or moves the registration to another session.
func (s *Service) resolvePeriodRegistrations(ctx context.Context, tx *sql.Tx, periodID int64, resolution string, targetSession int64) (moved int, refunded int, err error) {
	if resolution == "transfer" {
		var targetStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT s.exam_session_status
			FROM exam_sessions s
			LEFT JOIN exam_periods p ON p.exam_period_id = s.exam_session_exam_period_id
			WHERE s.exam_session_id = $1 AND s.exam_session_is_deleted = FALSE`, targetSession).Scan(&targetStatus); errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ValidationError{Msg: "Không tìm thấy ca thi đích để chuyển"}
		} else if err != nil {
			return 0, 0, fmt.Errorf("exam: load target session: %w", err)
		}
		if targetStatus == enum.SessionStatusDraft.String() ||
			targetStatus == enum.SessionStatusScheduled.String() ||
			targetStatus == enum.SessionStatusCancelled.String() ||
			targetStatus == enum.SessionStatusCompleted.String() {
			return 0, 0, ValidationError{Msg: "Ca thi đích không ở trạng thái nhận đăng ký"}
		}
	}

	// Release every seat held by this period's registrations.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status = 'available', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_is_deleted = FALSE
		  AND exam_seat_status IN ('held', 'booked')
		  AND exam_seat_exam_session_id IN (
		      SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_period_id = $1)`,
		periodID); err != nil {
		return 0, 0, fmt.Errorf("exam: release seats of cancelled period: %w", err)
	}

	if resolution == "refund" {
		res, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET payment_status = 'refunded', payment_updated_at = NOW()
			WHERE payment_is_deleted = FALSE
			  AND payment_status = 'success'
			  AND payment_exam_registration_id IN (
			      SELECT r.exam_registration_id
			      FROM exam_registrations r
			      JOIN exam_sessions s ON s.exam_session_id = r.exam_registration_exam_session_id
			      WHERE s.exam_session_exam_period_id = $1
			        AND r.exam_registration_is_deleted = FALSE)`,
			periodID)
		if err != nil {
			return 0, 0, fmt.Errorf("exam: refund payments of cancelled period: %w", err)
		}
		n, _ := res.RowsAffected()
		refunded = int(n)

		if _, err := tx.ExecContext(ctx, `
			UPDATE exam_registrations r
			SET exam_registration_status = 'cancelled', exam_registration_updated_at = NOW()
			FROM exam_sessions s
			WHERE s.exam_session_id = r.exam_registration_exam_session_id
			  AND s.exam_session_exam_period_id = $1
			  AND r.exam_registration_is_deleted = FALSE
			  AND r.exam_registration_status <> 'cancelled'`, periodID); err != nil {
			return 0, 0, fmt.Errorf("exam: cancel registrations of cancelled period: %w", err)
		}
		return 0, refunded, nil
	}

	// transfer: requeue registrations onto the target session and book a free seat there.
	res, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations r
		SET exam_registration_exam_session_id = $1, exam_registration_updated_at = NOW()
		FROM exam_sessions s
		WHERE s.exam_session_id = r.exam_registration_exam_session_id
		  AND s.exam_session_exam_period_id = $2
		  AND r.exam_registration_is_deleted = FALSE
		  AND r.exam_registration_status <> 'cancelled'`, targetSession, periodID)
	if err != nil {
		return 0, 0, fmt.Errorf("exam: transfer registrations: %w", err)
	}
	n, _ := res.RowsAffected()
	moved = int(n)

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id IN (
		    SELECT exam_seat_id FROM exam_seats
		    WHERE exam_seat_exam_session_id = $1
		      AND exam_seat_status = 'available'
		      AND exam_seat_is_deleted = FALSE
		    ORDER BY exam_seat_number ASC
		    LIMIT $2)`,
		targetSession, moved); err != nil {
		return 0, 0, fmt.Errorf("exam: book seats on target session: %w", err)
	}
	return moved, 0, nil
}

// AdminRescheduleExamPeriod postpones an entire exam period. The status never changes:
// the original date is preserved, the reschedule counter increases, child sessions move,
// and every affected candidate is notified.
func (s *Service) AdminRescheduleExamPeriod(ctx context.Context, periodID, actorID int64, req examreq.RescheduleExamPeriodRequestDTO) (response.RescheduleExamPeriodResponseDTO, error) {
	now := time.Now()
	if strings.TrimSpace(req.Reason) == "" {
		return response.RescheduleExamPeriodResponseDTO{}, ValidationError{Msg: "Lý do dời lịch là bắt buộc"}
	}

	newStartDate, err := time.Parse("2006-01-02", req.NewStartDate)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, ValidationError{Msg: "Ngày thi mới không hợp lệ (định dạng YYYY-MM-DD)"}
	}

	newEndDate := newStartDate
	if req.NewEndDate != "" {
		ed, err := time.Parse("2006-01-02", req.NewEndDate)
		if err != nil {
			return response.RescheduleExamPeriodResponseDTO{}, ValidationError{Msg: "Ngày kết thúc mới không hợp lệ (định dạng YYYY-MM-DD)"}
		}
		newEndDate = ed
	}

	var newDeadline *time.Time
	if req.NewRegistrationDeadline != "" {
		newDeadline, err = parseOptionalRFC3339(req.NewRegistrationDeadline)
		if err != nil {
			return response.RescheduleExamPeriodResponseDTO{}, ValidationError{Msg: "hạn đăng ký mới phải có định dạng RFC3339 hợp lệ"}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: begin reschedule tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	src, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionReschedule, st, now); err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, err
	}

	daysOffset := req.DaysOffset
	if daysOffset == 0 && src.ExamPeriodStartDate != nil {
		daysOffset = int(newStartDate.Sub(*src.ExamPeriodStartDate).Hours() / 24)
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_start_date = $1,
		    exam_period_end_date = $2,
		    exam_period_registration_deadline = COALESCE($3, exam_period_registration_deadline),
		    exam_period_original_date = COALESCE(exam_period_original_date, exam_period_start_date),
		    exam_period_reschedule_count = exam_period_reschedule_count + 1,
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $4
		RETURNING `+periodColumns,
		newStartDate, newEndDate, newDeadline, periodID,
	).Scan(periodScanDest(&updated)...); err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: update rescheduled period: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_date = COALESCE(exam_session_date + ($1 * INTERVAL '1 day'), $2::DATE),
		    exam_session_registration_deadline = COALESCE($3, exam_session_registration_deadline),
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $4
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status <> 'cancelled'`,
		daysOffset, newStartDate, newDeadline, periodID,
	)
	if err != nil {
		return response.RescheduleExamPeriodResponseDTO{}, fmt.Errorf("exam: cascade reschedule to sessions: %w", err)
	}
	sessionsCount, _ := res.RowsAffected()

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

	oldVal, _ := json.Marshal(map[string]any{
		"start_date": src.ExamPeriodStartDate,
		"end_date":   src.ExamPeriodEndDate,
		"deadline":   src.ExamPeriodRegistrationDeadline,
		"status":     src.ExamPeriodStatus.String(),
	})
	newVal, _ := json.Marshal(map[string]any{
		"new_start_date":     newStartDate,
		"new_end_date":       newEndDate,
		"new_deadline":       newDeadline,
		"reason":             req.Reason,
		"days_offset":        daysOffset,
		"candidates_alerted": candidatesAlerted,
		"reschedule_count":   updated.ExamPeriodRescheduleCount,
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
		Period:            ToExamPeriodResponseDTO(updated, newPeriodState(updated), int(sessionsCount), nil, now),
	}, nil
}

// AdminCloseRegistration closes a period's registration early and cascades the closure to
// every child session inside a single transaction.
func (s *Service) AdminCloseRegistration(ctx context.Context, periodID, actorID int64, req examreq.CloseRegistrationRequestDTO) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin close registration tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionCloseRegistration, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'registration_closed',
		    exam_period_registration_deadline = LEAST(COALESCE(exam_period_registration_deadline, NOW()), NOW()),
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $1
		RETURNING `+periodColumns, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: close period registration: %w", err)
	}

	// Open children follow the period; sessions closed for their own reason keep that reason.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'closed',
		    exam_session_closed_reason = 'period_closed',
		    exam_session_registration_deadline = LEAST(COALESCE(exam_session_registration_deadline, NOW()), NOW()),
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status = 'open'`, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: close child sessions registration: %w", err)
	}

	// Draft and scheduled children must not be published inside a closed period: cancel the schedules.
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'draft',
		    exam_session_publish_at = NULL,
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $1
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status = 'scheduled'`, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: cancel scheduled child sessions: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"period_id":     periodID,
		"reason":        req.Reason,
		"status":        enum.PeriodStatusRegistrationClosed.String(),
		"closed_reason": enum.ClosedReasonPeriodClosed.String(),
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
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminReopenPeriodRegistration reopens a closed period before the exam date. A new future
// deadline is mandatory; only sessions closed because of the period follow it back open.
func (s *Service) AdminReopenPeriodRegistration(ctx context.Context, periodID, actorID int64, req examreq.ReopenPeriodRegistrationRequestDTO) (response.ExamPeriodResponseDTO, error) {
	now := time.Now()
	newDeadline, err := parseOptionalRFC3339(req.RegistrationDeadline)
	if err != nil || newDeadline == nil {
		return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "Phải đặt hạn đăng ký mới (định dạng RFC3339)"}
	}
	if !newDeadline.After(now) {
		return response.ExamPeriodResponseDTO{}, ValidationError{Msg: "Hạn đăng ký mới phải ở trong tương lai"}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: begin reopen period tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, st, err := s.lockPeriod(ctx, tx, periodID)
	if err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}
	if err := GuardPeriodAction(ActionReopenRegistration, st, now); err != nil {
		return response.ExamPeriodResponseDTO{}, err
	}

	var updated ExamPeriodEntity
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_periods
		SET exam_period_status = 'registration_open',
		    exam_period_registration_deadline = $1,
		    exam_period_updated_at = NOW()
		WHERE exam_period_id = $2
		RETURNING `+periodColumns, newDeadline, periodID).Scan(periodScanDest(&updated)...); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: reopen period: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_status = 'open',
		    exam_session_closed_reason = NULL,
		    exam_session_registration_deadline = CASE
		        WHEN exam_session_registration_deadline IS NULL OR exam_session_registration_deadline <= NOW()
		        THEN $1 ELSE exam_session_registration_deadline END,
		    exam_session_updated_at = NOW()
		WHERE exam_session_exam_period_id = $2
		  AND exam_session_is_deleted = FALSE
		  AND exam_session_status = 'closed'
		  AND exam_session_closed_reason = 'period_closed'`, newDeadline, periodID); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: reopen period_closed sessions: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamPeriod.String(),
		EntityID:       periodID,
		NewValue:       json.RawMessage(fmt.Sprintf(`{"status":"registration_open","registration_deadline":%q}`, newDeadline.Format(time.RFC3339))),
	})

	if err := tx.Commit(); err != nil {
		return response.ExamPeriodResponseDTO{}, fmt.Errorf("exam: commit reopen period: %w", err)
	}
	s.invalidateSessionCache(ctx)
	return ToExamPeriodResponseDTO(updated, newPeriodState(updated), 0, nil, now), nil
}

// AdminUpdateExamSession updates an exam session. Schedule fields (date, shift, fee,
// registration deadline) are locked once the session has registrations; only the name and
// description stay editable, and the whole edit is refused for completed or cancelled sessions.
func (s *Service) AdminUpdateExamSession(ctx context.Context, sessionID, actorID int64, req examreq.UpdateExamSessionRequestDTO) (response.ExamSessionResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin update session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	e, st, err := s.lockSession(ctx, tx, sessionID)
	if err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if err := GuardSessionAction(ActionEdit, st, now); err != nil {
		return response.ExamSessionResponseDTO{}, err
	}
	if !req.HasAnyChange() {
		return response.ExamSessionResponseDTO{}, ValidationError{Msg: "Không có thông tin nào cần cập nhật"}
	}

	scheduleEditable := st.CanEditSessionSchedule(now)
	if req.HasScheduleChange() && !scheduleEditable {
		return response.ExamSessionResponseDTO{}, ErrSessionNotEditable
	}

	name := e.ExamSessionName
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		name = &trimmed
	}
	description := e.ExamSessionDescription
	if req.Description != nil {
		description = req.Description
	}

	sessionDate := e.ExamSessionDate
	if req.Date != nil && scheduleEditable {
		d, err := parseDate(*req.Date)
		if err != nil {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "định dạng ngày thi không hợp lệ (YYYY-MM-DD)"}
		}
		sessionDate = d
	}

	shift := e.ExamSessionShift
	if req.Shift != nil && scheduleEditable {
		sh := enum.ExamShift(*req.Shift)
		if !sh.Valid() {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "ca thi không hợp lệ (morning, afternoon, evening)"}
		}
		shift = sh
	}

	fee := e.ExamSessionFee
	if req.Fee != nil && scheduleEditable {
		if *req.Fee < 0 {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "lệ phí thi không được âm"}
		}
		fee = *req.Fee
	}

	deadline := e.ExamSessionRegistrationDeadline
	if req.RegistrationDeadline != nil && scheduleEditable {
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

	capacity := e.ExamSessionCapacity
	newStatus := e.ExamSessionStatus
	newReason := e.ExamSessionClosedReason
	if req.Capacity != nil {
		if *req.Capacity <= 0 {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: "sức chứa ca thi phải lớn hơn 0"}
		}
		if *req.Capacity < st.RegisteredCount {
			return response.ExamSessionResponseDTO{}, ValidationError{Msg: fmt.Sprintf(
				"Không thể giảm sĩ số xuống %d vì đã có %d thí sinh đăng ký", *req.Capacity, st.RegisteredCount)}
		}
		capacity = req.Capacity
		// Raising the capacity of a full session reopens it automatically.
		if st.ClosedReason == enum.ClosedReasonFull && *req.Capacity > st.RegisteredCount {
			newStatus = enum.SessionStatusOpen
			newReason = ""
		}
	}

	var updated ExamSessionEntity
	err = scanSession(tx.QueryRowContext(ctx, `
		UPDATE exam_sessions
		SET exam_session_name = $1,
		    exam_session_description = $2,
		    exam_session_date = $3,
		    exam_session_shift = $4,
		    exam_session_capacity = $5,
		    exam_session_fee = $6,
		    exam_session_registration_deadline = $7,
		    exam_session_check_in_at = $8,
		    exam_session_status = $9,
		    exam_session_closed_reason = $10,
		    exam_session_updated_at = NOW()
		WHERE exam_session_id = $11
		RETURNING `+sessionColumns,
		name, description, sessionDate, shift, capacity, fee, deadline, checkInAt,
		newStatus, nullableReason(newReason), sessionID,
	), &updated)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: update session: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"date":       updated.ExamSessionDate,
		"shift":      updated.ExamSessionShift.String(),
		"capacity":   updated.ExamSessionCapacity,
		"fee":        updated.ExamSessionFee,
		"status":     updated.ExamSessionStatus.String(),
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

	st.StoredStatus = updated.ExamSessionStatus
	st.ClosedReason = updated.ExamSessionClosedReason
	st.Date = updated.ExamSessionDate
	st.Deadline = updated.ExamSessionRegistrationDeadline
	st.Capacity = updated.ExamSessionCapacity
	return ToExamSessionResponseDTO(updated, st, now), nil
}

// AdminDuplicateExamSession duplicates an individual exam session within the same exam period.
func (s *Service) AdminDuplicateExamSession(ctx context.Context, sessionID, actorID int64, req examreq.DuplicateExamSessionRequestDTO) (response.ExamSessionResponseDTO, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: begin duplicate session tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

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

	if src.ExamSessionExamPeriodID != nil {
		var periodStatus string
		err = tx.QueryRowContext(ctx, `
			SELECT exam_period_status
			FROM exam_periods
			WHERE exam_period_id = $1 AND exam_period_is_deleted = FALSE`,
			*src.ExamSessionExamPeriodID,
		).Scan(&periodStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return response.ExamSessionResponseDTO{}, ErrExamPeriodNotFound
		} else if err != nil {
			return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: check parent period status: %w", err)
		}
		if periodStatus == enum.PeriodStatusRegistrationClosed.String() ||
			periodStatus == enum.PeriodStatusCancelled.String() ||
			periodStatus == enum.PeriodStatusCompleted.String() {
			return response.ExamSessionResponseDTO{}, ErrParentPeriodClosed
		}
	}

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
			baseName = fmt.Sprintf("%s - %s", typeName, strings.ToUpper(string(src.ExamSessionShift)))
		}
		newName = formatDuplicateSessionName(baseName)
	}

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
			exam_session_publish_at,
			exam_session_check_in_at,
			exam_session_duplicated_from_id
		) VALUES (
			$1, $2, $3, $4, $5, NULL, $6, NULL, $7, NULL, 'draft', NULL, NULL, $8
		)
		RETURNING `+sessionColumns,
		src.ExamSessionExamPeriodID,
		src.ExamSessionExamTypeID,
		src.ExamSessionExamRoomID,
		newName,
		src.ExamSessionDescription,
		src.ExamSessionShift,
		src.ExamSessionFee,
		src.ExamSessionID,
	).Scan(sessionScanDest(&created, new(sql.NullString))...)
	if err != nil {
		return response.ExamSessionResponseDTO{}, fmt.Errorf("exam: insert duplicated session: %w", err)
	}

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
	return ToExamSessionResponseDTO(created, newSessionState(created, "", nil, nil, nil, 0), now), nil
}

// newSessionState builds guard state for a freshly written session row.
func newSessionState(e ExamSessionEntity, parent enum.ExamPeriodStatus, parentRegStart, parentRegDeadline, parentEnd *time.Time, headCount int) SessionState {
	return SessionState{
		StoredStatus:      e.ExamSessionStatus,
		ClosedReason:      e.ExamSessionClosedReason,
		PublishAt:         e.ExamSessionPublishAt,
		Date:              e.ExamSessionDate,
		Deadline:          e.ExamSessionRegistrationDeadline,
		Capacity:          e.ExamSessionCapacity,
		RegisteredCount:   headCount,
		PeriodStatus:      parent,
		PeriodRegStart:    parentRegStart,
		PeriodRegDeadline: parentRegDeadline,
		PeriodEndDate:     parentEnd,
	}
}

// nullableReason converts an unset closed reason into a SQL NULL.
func nullableReason(r enum.ExamSessionClosedReason) *string {
	if r == "" {
		return nil
	}
	s := r.String()
	return &s
}

// ensureParentPeriodPublishable rejects publishing a session whose parent period cannot
// expose it to candidates.
func ensureParentPeriodPublishable(parent enum.ExamPeriodStatus) error {
	switch parent {
	case "", enum.PeriodStatusPublished, enum.PeriodStatusRegistrationOpen, enum.PeriodStatusRegistrationClosed:
		return nil
	case enum.PeriodStatusDraft:
		return &InvalidStateTransitionError{
			Action: ActionPublish,
			Status: parent.String(),
			Reason: "Đợt thi đang ở trạng thái nháp; phải công bố đợt thi trước khi công khai ca thi",
		}
	case enum.PeriodStatusCancelled:
		return &InvalidStateTransitionError{
			Action: ActionPublish,
			Status: parent.String(),
			Reason: "Đợt thi đã bị hủy; không thể công khai ca thi",
		}
	default:
		return &InvalidStateTransitionError{
			Action: ActionPublish,
			Status: parent.String(),
			Reason: "Đợt thi đã hoàn tất; không thể công khai ca thi",
		}
	}
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("giá trị thời gian phải có định dạng RFC3339 hợp lệ")
	}
	return &t, nil
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
