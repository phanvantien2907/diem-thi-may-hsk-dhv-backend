package tests

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/exam"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/enum"
)

// sessionRowColumns must match the projection produced by exam.sessionSelect:
// sessionColumns + parent columns + head count.
var sessionRowColumns = []string{
	"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
	"exam_session_name", "exam_session_description",
	"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
	"exam_session_registration_deadline", "exam_session_status", "exam_session_closed_reason",
	"exam_session_publish_at", "exam_session_check_in_at", "exam_session_duplicated_from_id",
	"exam_period_status", "exam_period_registration_start", "exam_period_registration_deadline",
	"exam_period_end_date", "head_count",
}

// sessionOnlyColumns matches a bare `RETURNING sessionColumns` projection.
var sessionOnlyColumns = sessionRowColumns[:15]

// periodOnlyColumns matches a bare `RETURNING periodColumns` projection.
var periodOnlyColumns = periodRowColumns[:14]

// periodRowColumns must match exam.periodSelect.
var periodRowColumns = []string{
	"exam_period_id", "exam_period_name", "exam_period_code",
	"exam_period_description", "exam_period_instructions",
	"exam_period_start_date", "exam_period_end_date",
	"exam_period_registration_start", "exam_period_registration_deadline",
	"exam_period_status", "exam_period_original_date", "exam_period_reschedule_count",
	"exam_period_created_at", "exam_period_updated_at",
	"session_count", "valid_session_count", "registration_count",
}

const lockSessionSQL = `SELECT es\.exam_session_id.*FOR UPDATE OF es`
const lockPeriodSQL = `SELECT ep\.exam_period_id.*FOR UPDATE OF ep`

// sessionRow builds a session row for the canonical projection.
type sessionRow struct {
	id            int64
	periodID      *int64
	examTypeID    int64
	date          *time.Time
	shift         enum.ExamShift
	capacity      *int
	fee           int64
	deadline      *time.Time
	status        enum.ExamSessionStatus
	closedReason  *string
	publishAt     *time.Time
	checkInAt     *time.Time
	duplicatedID  *int64
	periodStatus  *string
	periodRegFrom *time.Time
	periodRegTo   *time.Time
	periodEndDate *time.Time
	headCount     int
}

func (r sessionRow) values() []driver.Value {
	return []driver.Value{
		r.id, r.periodID, r.examTypeID, nil, nil,
		r.date, r.shift, r.capacity, r.fee,
		r.deadline, r.status, r.closedReason,
		r.publishAt, r.checkInAt, r.duplicatedID,
		r.periodStatus, r.periodRegFrom, r.periodRegTo,
		r.periodEndDate, r.headCount,
	}
}

func newMockService(t *testing.T) (*exam.Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return exam.NewService(db, fakeAuditService{}), mock
}

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func TestPublishExamSession_SetsOpenWhenScheduleComplete(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)
	periodID := int64(11)

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, periodID: &periodID, examTypeID: 3,
			date: &date, shift: enum.ShiftMorning, capacity: intPtr(30), fee: 500000,
			deadline: &deadline, status: enum.SessionStatusDraft,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date,
		}.values()...))
	mock.ExpectQuery(`UPDATE exam_sessions\s+SET exam_session_status = \$1`).
		WithArgs(enum.SessionStatusOpen, nil, int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionOnlyColumns).AddRow(sessionRow{
			id: 7, periodID: &periodID, examTypeID: 3,
			date: &date, shift: enum.ShiftMorning, capacity: intPtr(30), fee: 500000,
			deadline: &deadline, status: enum.SessionStatusOpen,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date,
		}.values()[:15]...))
	mock.ExpectCommit()

	got, err := svc.PublishExamSession(context.Background(), 7, 42, examreq.PublishSessionRequestDTO{})
	if err != nil {
		t.Fatalf("PublishExamSession() error = %v", err)
	}
	if got.Session.Status != string(enum.SessionStatusOpen) {
		t.Fatalf("status = %q, want open", got.Session.Status)
	}
	if got.PublishAt != nil {
		t.Fatalf("publish_at should be nil for an immediate publish")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishExamSession_SchedulesWithFutureTime(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(72 * time.Hour)
	deadline := now.Add(48 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	publishAt := now.Add(2 * time.Hour).Truncate(time.Second)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &deadline, status: enum.SessionStatusDraft,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date,
		}.values()...))
	mock.ExpectQuery(`UPDATE exam_sessions\s+SET exam_session_status = \$1`).
		WithArgs(enum.SessionStatusScheduled, sqlmock.AnyArg(), int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionOnlyColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &deadline, status: enum.SessionStatusScheduled,
			publishAt: &publishAt, periodStatus: &periodStatus, periodRegFrom: &regStart,
			periodRegTo: &deadline, periodEndDate: &date,
		}.values()[:15]...))
	mock.ExpectCommit()

	got, err := svc.PublishExamSession(context.Background(), 7, 42, examreq.PublishSessionRequestDTO{
		PublishAt: publishAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("PublishExamSession() error = %v", err)
	}
	if got.Session.Status != string(enum.SessionStatusScheduled) {
		t.Fatalf("status = %q, want scheduled", got.Session.Status)
	}
	if got.PublishAt == nil {
		t.Fatal("publish_at should be echoed back")
	}
}

func TestPublishExamSession_RejectsIncompleteSchedule(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)

	mock.ExpectBegin()
	// capacity and deadline are NULL because the session is a fresh duplicate
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			status: enum.SessionStatusDraft, periodStatus: &periodStatus,
		}.values()...))
	mock.ExpectRollback()

	_, err := svc.PublishExamSession(context.Background(), 7, 42, examreq.PublishSessionRequestDTO{})
	if !errors.Is(err, exam.ErrSessionScheduleIncomplete) {
		t.Fatalf("error = %v, want ErrSessionScheduleIncomplete", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishExamSession_RejectsAlreadyOpenSession(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &deadline, status: enum.SessionStatusOpen,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date,
		}.values()...))
	mock.ExpectRollback()

	_, err := svc.PublishExamSession(context.Background(), 7, 42, examreq.PublishSessionRequestDTO{})
	if !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %v, want InvalidStateTransition", err)
	}
}

func TestPublishExamSession_RejectsPastPublishTime(t *testing.T) {
	svc, _ := newMockService(t)
	_, err := svc.PublishExamSession(context.Background(), 7, 42, examreq.PublishSessionRequestDTO{
		PublishAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
	})
	if !exam.IsValidationError(err) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestCloseSessionRegistration_DefaultsToManualReason(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &deadline, status: enum.SessionStatusOpen,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date,
		}.values()...))
	mock.ExpectQuery(`UPDATE exam_sessions\s+SET exam_session_status = 'closed'`).
		WithArgs(enum.ClosedReasonManual, int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionOnlyColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &now, status: enum.SessionStatusClosed,
			closedReason: strPtr("manual"), periodStatus: &periodStatus,
			periodRegFrom: &regStart, periodRegTo: &deadline, periodEndDate: &date,
		}.values()[:15]...))
	mock.ExpectCommit()

	got, err := svc.CloseSessionRegistration(context.Background(), 7, 42, examreq.CloseRegistrationRequestDTO{
		Reason: "Tạm dừng nhận hồ sơ",
	})
	if err != nil {
		t.Fatalf("CloseSessionRegistration() error = %v", err)
	}
	if got.ClosedReason == nil || *got.ClosedReason != "manual" {
		t.Fatalf("closed_reason = %v, want manual", got.ClosedReason)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReopenSessionRegistration_RequiresCapacityIncreaseWhenFull(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)
	capacity := 30

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: &capacity, deadline: &deadline, status: enum.SessionStatusClosed,
			closedReason: strPtr("full"), periodStatus: &periodStatus,
			periodRegFrom: &regStart, periodRegTo: &deadline, periodEndDate: &date,
			headCount: capacity,
		}.values()...))
	mock.ExpectRollback()

	_, err := svc.ReopenSessionRegistration(context.Background(), 7, 42, examreq.ReopenSessionRegistrationRequestDTO{})
	if !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %v, want InvalidStateTransition", err)
	}
}

func TestReopenSessionRegistration_RequiresNewDeadlineWhenExpired(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	pastDeadline := now.Add(-time.Hour)
	regStart := now.Add(-72 * time.Hour)
	periodDeadline := now.Add(24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(30), deadline: &pastDeadline, status: enum.SessionStatusClosed,
			closedReason: strPtr("deadline"), periodStatus: &periodStatus,
			periodRegFrom: &regStart, periodRegTo: &periodDeadline, periodEndDate: &date,
		}.values()...))
	mock.ExpectRollback()

	_, err := svc.ReopenSessionRegistration(context.Background(), 7, 42, examreq.ReopenSessionRegistrationRequestDTO{})
	if !exam.IsValidationError(err) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestUpdateSessionCapacity_ReopensFullSession(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)
	capacity := 30

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: &capacity, deadline: &deadline, status: enum.SessionStatusClosed,
			closedReason: strPtr("full"), periodStatus: &periodStatus,
			periodRegFrom: &regStart, periodRegTo: &deadline, periodEndDate: &date,
			headCount: capacity,
		}.values()...))
	mock.ExpectQuery(`UPDATE exam_sessions\s+SET exam_session_capacity = \$1`).
		WithArgs(40, enum.SessionStatusOpen, nil, int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionOnlyColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: intPtr(40), deadline: &deadline, status: enum.SessionStatusOpen,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date, headCount: capacity,
		}.values()[:15]...))
	mock.ExpectCommit()

	got, err := svc.UpdateSessionCapacity(context.Background(), 7, 42, examreq.UpdateCapacityRequestDTO{Capacity: 40})
	if err != nil {
		t.Fatalf("UpdateSessionCapacity() error = %v", err)
	}
	if got.Status != string(enum.SessionStatusOpen) || got.ClosedReason != nil {
		t.Fatalf("expected reopened session, got status=%q reason=%v", got.Status, got.ClosedReason)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSessionCapacity_RejectsBelowRegisteredCount(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	date := now.Add(48 * time.Hour)
	deadline := now.Add(24 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	periodStatus := string(enum.PeriodStatusRegistrationOpen)
	capacity := 30

	mock.ExpectBegin()
	mock.ExpectQuery(lockSessionSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(sessionRowColumns).AddRow(sessionRow{
			id: 7, examTypeID: 3, date: &date, shift: enum.ShiftMorning,
			capacity: &capacity, deadline: &deadline, status: enum.SessionStatusOpen,
			periodStatus: &periodStatus, periodRegFrom: &regStart, periodRegTo: &deadline,
			periodEndDate: &date, headCount: 20,
		}.values()...))
	mock.ExpectRollback()

	_, err := svc.UpdateSessionCapacity(context.Background(), 7, 42, examreq.UpdateCapacityRequestDTO{Capacity: 10})
	if !exam.IsValidationError(err) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestAdminCloseRegistration_CascadesPeriodClosedAndCancelsSchedules(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows(periodRowColumns).AddRow(
			11, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
			&start, &end, &regStart, &deadline,
			enum.PeriodStatusRegistrationOpen, nil, 0, now, now,
			3, 2, 5,
		))
	mock.ExpectQuery(`UPDATE exam_periods\s+SET exam_period_status = 'registration_closed'`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows(periodOnlyColumns).AddRow(
			11, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
			&start, &end, &regStart, &now,
			enum.PeriodStatusRegistrationClosed, nil, 0, now, now,
		))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'closed'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'draft'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.AdminCloseRegistration(context.Background(), 11, 42, examreq.CloseRegistrationRequestDTO{
		Reason: "Đủ hồ sơ",
	})
	if err != nil {
		t.Fatalf("AdminCloseRegistration() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusRegistrationClosed) {
		t.Fatalf("status = %q, want registration_closed", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminReopenPeriodRegistration_RequiresNewFutureDeadline(t *testing.T) {
	svc, _ := newMockService(t)

	if _, err := svc.AdminReopenPeriodRegistration(context.Background(), 11, 42,
		examreq.ReopenPeriodRegistrationRequestDTO{}); !exam.IsValidationError(err) {
		t.Fatalf("missing deadline error = %v, want ValidationError", err)
	}
	if _, err := svc.AdminReopenPeriodRegistration(context.Background(), 11, 42,
		examreq.ReopenPeriodRegistrationRequestDTO{
			RegistrationDeadline: time.Now().Add(-time.Hour).Format(time.RFC3339),
		}); !exam.IsValidationError(err) {
		t.Fatalf("past deadline error = %v, want ValidationError", err)
	}
}

func TestAdminReopenPeriodRegistration_ReopensOnlyPeriodClosedSessions(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	regStart := now.Add(-48 * time.Hour)
	closedDeadline := now.Add(-time.Hour)
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	newDeadline := now.Add(48 * time.Hour).Truncate(time.Second)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows(periodRowColumns).AddRow(
			11, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
			&start, &end, &regStart, &closedDeadline,
			enum.PeriodStatusRegistrationClosed, nil, 0, now, now,
			3, 2, 5,
		))
	mock.ExpectQuery(`UPDATE exam_periods\s+SET exam_period_status = 'registration_open'`).
		WithArgs(sqlmock.AnyArg(), int64(11)).
		WillReturnRows(sqlmock.NewRows(periodOnlyColumns).AddRow(
			11, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
			&start, &end, &regStart, &newDeadline,
			enum.PeriodStatusRegistrationOpen, nil, 0, now, now,
		))
	mock.ExpectExec(`AND exam_session_closed_reason = 'period_closed'`).
		WithArgs(sqlmock.AnyArg(), int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	got, err := svc.AdminReopenPeriodRegistration(context.Background(), 11, 42,
		examreq.ReopenPeriodRegistrationRequestDTO{
			RegistrationDeadline: newDeadline.Format(time.RFC3339),
		})
	if err != nil {
		t.Fatalf("AdminReopenPeriodRegistration() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusRegistrationOpen) {
		t.Fatalf("status = %q, want registration_open", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminCloseRegistration_RejectsDraftPeriod(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows(periodRowColumns).AddRow(
			11, "Đợt thi nháp", "HSK-2026-07", nil, nil,
			nil, nil, nil, nil,
			enum.PeriodStatusDraft, nil, 0, now, now,
			1, 1, 0,
		))
	mock.ExpectRollback()

	_, err := svc.AdminCloseRegistration(context.Background(), 11, 42, examreq.CloseRegistrationRequestDTO{})
	if !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %v, want InvalidStateTransition", err)
	}
}

func TestGetExamSession_NotFound(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery(`SELECT es\.exam_session_id.*WHERE es\.exam_session_id = \$1`).
		WithArgs(int64(404)).
		WillReturnError(sql.ErrNoRows)

	_, err := svc.GetExamSession(context.Background(), 404)
	if !errors.Is(err, exam.ErrExamSessionNotFound) {
		t.Fatalf("error = %v, want ErrExamSessionNotFound", err)
	}
}
