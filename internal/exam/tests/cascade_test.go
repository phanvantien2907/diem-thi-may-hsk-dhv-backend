package tests

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/exam"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/enum"
)

// cascadePeriodRow builds the lockPeriod projection for a period that is being
// acted on right now, so its effective status must not depend on the clock.
func cascadePeriodRow(id int64, status enum.ExamPeriodStatus, start, end, regStart, regDeadline *time.Time, valid, regs int64) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(periodRowColumns).AddRow(
		id, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
		start, end, regStart, regDeadline,
		status, nil, 0, now, now,
		valid+1, valid, regs,
	)
}

// cascadePeriodOnlyRow builds the bare `RETURNING periodColumns` projection.
func cascadePeriodOnlyRow(id int64, status enum.ExamPeriodStatus, start, end, regStart, regDeadline *time.Time) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(periodOnlyColumns).AddRow(
		id, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
		start, end, regStart, regDeadline,
		status, nil, 0, now, now,
	)
}

// TestAdminCompleteExamPeriod_CompletesOpenAndClosedSessions covers the
// registration_closed → completed transition and its cascade.
func TestAdminCompleteExamPeriod_CompletesOpenAndClosedSessions(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(-24 * time.Hour)
	end := now.Add(24 * time.Hour)
	regStart := now.Add(-96 * time.Hour)
	deadline := now.Add(-48 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationClosed, &start, &end, &regStart, &deadline, 2, 5))
	mock.ExpectQuery(`UPDATE exam_periods(?: ep)?\s+SET exam_period_status = 'completed'`).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodOnlyRow(11, enum.PeriodStatusCompleted, &start, &end, &regStart, &deadline))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'completed'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	got, err := svc.AdminCompleteExamPeriod(context.Background(), 11, 42)
	if err != nil {
		t.Fatalf("AdminCompleteExamPeriod() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusCompleted) {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminCompleteExamPeriod_RejectsDraftPeriod proves the guard blocks the
// transition before any row is written.
func TestAdminCompleteExamPeriod_RejectsDraftPeriod(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(48 * time.Hour)
	end := now.Add(72 * time.Hour)
	regStart := now.Add(24 * time.Hour)
	deadline := now.Add(36 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusDraft, &start, &end, &regStart, &deadline, 1, 0))
	mock.ExpectRollback()

	if _, err := svc.AdminCompleteExamPeriod(context.Background(), 11, 42); !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %v, want InvalidStateTransitionError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminUnpublishExamPeriod_CancelsScheduledSessions covers
// published → draft plus the schedule cancellation cascade.
func TestAdminUnpublishExamPeriod_CancelsScheduledSessions(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(48 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusPublished, &start, &end, &regStart, &deadline, 2, 0))
	mock.ExpectQuery(`UPDATE exam_periods(?: ep)?\s+SET exam_period_status = 'draft'`).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodOnlyRow(11, enum.PeriodStatusDraft, &start, &end, &regStart, &deadline))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'draft', exam_session_publish_at = NULL`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.AdminUnpublishExamPeriod(context.Background(), 11, 42)
	if err != nil {
		t.Fatalf("AdminUnpublishExamPeriod() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusDraft) {
		t.Fatalf("status = %q, want draft", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminCancelExamPeriod_RefundsRegistrations covers the refund resolution:
// every child session is cancelled, seats are released, payments are refunded
// and registrations are cancelled.
func TestAdminCancelExamPeriod_RefundsRegistrations(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationOpen, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectQuery(`UPDATE exam_periods(?: ep)?\s+SET exam_period_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodOnlyRow(11, enum.PeriodStatusCancelled, &start, &end, &regStart, &deadline))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE exam_seats\s+SET exam_seat_status = 'available'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectExec(`UPDATE payments\s+SET payment_status = 'refunded'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(`UPDATE exam_registrations r\s+SET exam_registration_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	got, err := svc.AdminCancelExamPeriod(context.Background(), 11, 42, examreq.CancelPeriodRequestDTO{
		Reason:     "Bão lũ, không thể tổ chức thi",
		Resolution: "refund",
	})
	if err != nil {
		t.Fatalf("AdminCancelExamPeriod() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusCancelled) {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminCancelExamPeriod_TransfersRegistrations covers the transfer
// resolution: the target session must be usable, then registrations move and
// free seats there are booked.
func TestAdminCancelExamPeriod_TransfersRegistrations(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationOpen, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectQuery(`UPDATE exam_periods(?: ep)?\s+SET exam_period_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodOnlyRow(11, enum.PeriodStatusCancelled, &start, &end, &regStart, &deadline))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectQuery(`SELECT s\.exam_session_status`).
		WithArgs(int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{"exam_session_status"}).AddRow("open"))
	mock.ExpectExec(`UPDATE exam_seats\s+SET exam_seat_status = 'available'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectExec(`UPDATE exam_registrations r\s+SET exam_registration_exam_session_id`).
		WithArgs(int64(77), int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(`UPDATE exam_seats\s+SET exam_seat_status = 'booked'`).
		WithArgs(int64(77), 3).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	got, err := svc.AdminCancelExamPeriod(context.Background(), 11, 42, examreq.CancelPeriodRequestDTO{
		Reason:        "Sự cố phòng thi, chuyển sang ca khác",
		Resolution:    "transfer",
		TargetSession: 77,
	})
	if err != nil {
		t.Fatalf("AdminCancelExamPeriod() error = %v", err)
	}
	if got.Status != string(enum.PeriodStatusCancelled) {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminCancelExamPeriod_RejectsUnusableTargetSession proves a draft target
// session aborts the whole transaction.
func TestAdminCancelExamPeriod_RejectsUnusableTargetSession(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationOpen, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectQuery(`UPDATE exam_periods(?: ep)?\s+SET exam_period_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodOnlyRow(11, enum.PeriodStatusCancelled, &start, &end, &regStart, &deadline))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_status = 'cancelled'`).
		WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectQuery(`SELECT s\.exam_session_status`).
		WithArgs(int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{"exam_session_status"}).AddRow("draft"))
	mock.ExpectRollback()

	_, err := svc.AdminCancelExamPeriod(context.Background(), 11, 42, examreq.CancelPeriodRequestDTO{
		Reason:        "Sự cố phòng thi, chuyển sang ca khác",
		Resolution:    "transfer",
		TargetSession: 77,
	})
	if !exam.IsValidationError(err) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminCancelExamPeriod_RequiresResolution covers the mandatory
// refund-or-transfer choice.
func TestAdminCancelExamPeriod_RequiresResolution(t *testing.T) {
	svc, _ := newMockService(t)

	for _, req := range []examreq.CancelPeriodRequestDTO{
		{Reason: "Huỷ đợt thi vì bão"},
		{Reason: "Huỷ đợt thi vì bão", Resolution: "cash"},
		{Reason: "Huỷ đợt thi vì bão", Resolution: "transfer"},
	} {
		if _, err := svc.AdminCancelExamPeriod(context.Background(), 11, 42, req); !exam.IsValidationError(err) {
			t.Fatalf("resolution %q target %d error = %v, want ValidationError", req.Resolution, req.TargetSession, err)
		}
	}
}

// TestAdminRescheduleExamPeriod_KeepsStatusAndShiftsChildren covers the rule
// that rescheduling never changes a status: it only records the original date,
// bumps the counter, shifts child sessions and notifies candidates.
func TestAdminRescheduleExamPeriod_KeepsStatusAndShiftsChildren(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(96 * time.Hour)
	end := now.Add(120 * time.Hour)
	regStart := now.Add(-24 * time.Hour)
	deadline := now.Add(72 * time.Hour)

	newStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 3)
	newEnd := newStart.AddDate(0, 0, 1)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationOpen, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectQuery(`UPDATE exam_periods ep\s+SET exam_period_start_date`).
		WithArgs(newStart, newEnd, nil, int64(11)).
		WillReturnRows(sqlmock.NewRows(periodOnlyColumns).AddRow(
			11, "Đợt thi tháng 6", "HSK-2026-06", nil, nil,
			&newStart, &newEnd, &regStart, &deadline,
			enum.PeriodStatusRegistrationOpen, &start, 1, now, now,
		))
	mock.ExpectExec(`UPDATE exam_sessions\s+SET exam_session_date = COALESCE`).
		WithArgs(3, newStart, nil, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(`INSERT INTO notifications`).
		WithArgs("Đợt thi tháng 6", newStart.Format("02/01/2006"), "Mưa bão ảnh hưởng địa điểm thi", int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	got, err := svc.AdminRescheduleExamPeriod(context.Background(), 11, 42, examreq.RescheduleExamPeriodRequestDTO{
		NewStartDate: newStart.Format("2006-01-02"),
		NewEndDate:   newEnd.Format("2006-01-02"),
		DaysOffset:   3,
		Reason:       "Mưa bão ảnh hưởng địa điểm thi",
	})
	if err != nil {
		t.Fatalf("AdminRescheduleExamPeriod() error = %v", err)
	}
	if got.PeriodID != 11 || got.UpdatedSessions != 3 || got.CandidatesAlerted != 5 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.Period.Status != string(enum.PeriodStatusRegistrationOpen) {
		t.Fatalf("status = %q, want registration_open (reschedule never changes status)", got.Period.Status)
	}
	if got.Period.OriginalDate == nil {
		t.Fatal("original_date was not recorded")
	}
	if got.Period.RescheduleCount != 1 {
		t.Fatalf("reschedule_count = %d, want 1", got.Period.RescheduleCount)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestAdminRescheduleExamPeriod_Validation covers the pre-transaction checks.
func TestAdminRescheduleExamPeriod_Validation(t *testing.T) {
	svc, _ := newMockService(t)

	cases := []struct {
		name string
		req  examreq.RescheduleExamPeriodRequestDTO
	}{
		{"missing reason", examreq.RescheduleExamPeriodRequestDTO{NewStartDate: "2026-07-01"}},
		{"bad start date", examreq.RescheduleExamPeriodRequestDTO{NewStartDate: "01-07-2026", Reason: "Mưa bão lớn"}},
		{"bad end date", examreq.RescheduleExamPeriodRequestDTO{NewStartDate: "2026-07-01", NewEndDate: "02/07/2026", Reason: "Mưa bão lớn"}},
		{"bad deadline", examreq.RescheduleExamPeriodRequestDTO{NewStartDate: "2026-07-01", NewRegistrationDeadline: "tomorrow", Reason: "Mưa bão lớn"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.AdminRescheduleExamPeriod(context.Background(), 11, 42, tc.req); !exam.IsValidationError(err) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
		})
	}
}

// TestAdminCloseRegistration_RejectsClosedPeriod proves the guard runs before
// the cascade writes anything.
func TestAdminCloseRegistration_RejectsClosedPeriod(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	start := now.Add(48 * time.Hour)
	end := now.Add(72 * time.Hour)
	regStart := now.Add(-96 * time.Hour)
	deadline := now.Add(-24 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(lockPeriodSQL).
		WithArgs(int64(11)).
		WillReturnRows(cascadePeriodRow(11, enum.PeriodStatusRegistrationClosed, &start, &end, &regStart, &deadline, 2, 4))
	mock.ExpectRollback()

	if _, err := svc.AdminCloseRegistration(context.Background(), 11, 42, examreq.CloseRegistrationRequestDTO{
		Reason: "Đã đủ hồ sơ",
	}); !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %v, want InvalidStateTransitionError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
