package tests

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/exam/enum"
)

// ---------------------------------------------------------------------------
// Action matrices
//
// The tables below are a 1:1 transcription of the business spec:
//
//	period: draft → published → registration_open → registration_closed → completed (+cancelled)
//	session: draft → scheduled → open → closed → completed (+cancelled)
//
// Every cell is asserted through the public guard (not through the unexported
// denial helpers) so the matrix also protects the 409 contract the API exposes.
// ---------------------------------------------------------------------------

var allPeriodStatuses = []enum.ExamPeriodStatus{
	enum.PeriodStatusDraft,
	enum.PeriodStatusPublished,
	enum.PeriodStatusRegistrationOpen,
	enum.PeriodStatusRegistrationClosed,
	enum.PeriodStatusCompleted,
	enum.PeriodStatusCancelled,
}

var allSessionStatuses = []enum.ExamSessionStatus{
	enum.SessionStatusDraft,
	enum.SessionStatusScheduled,
	enum.SessionStatusOpen,
	enum.SessionStatusClosed,
	enum.SessionStatusCompleted,
	enum.SessionStatusCancelled,
}

// allowOnly turns a compact status list into a lookup map for one matrix row.
func allowOnly(statuses ...enum.ExamPeriodStatus) map[enum.ExamPeriodStatus]bool {
	set := make(map[enum.ExamPeriodStatus]bool, len(statuses))
	for _, s := range statuses {
		set[s] = true
	}
	return set
}

// periodStateFor builds a PeriodState whose effective status at fixedNow equals
// the requested status. Window fields are chosen so that EffectivePeriodStatus
// derives the expected value rather than falling back to the stored column.
func periodStateFor(status enum.ExamPeriodStatus) exam.PeriodState {
	switch status {
	case enum.PeriodStatusDraft:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(5), RegistrationDeadline: day(10),
			StartDate: day(15), EndDate: day(18), ValidSessionCount: 1, RegistrationCount: 0,
		}
	case enum.PeriodStatusPublished:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(5), RegistrationDeadline: day(10),
			StartDate: day(15), EndDate: day(18), ValidSessionCount: 1, RegistrationCount: 0,
		}
	case enum.PeriodStatusRegistrationOpen:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(-5), RegistrationDeadline: day(5),
			StartDate: day(10), EndDate: day(12), ValidSessionCount: 1, RegistrationCount: 4,
		}
	case enum.PeriodStatusRegistrationClosed:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(-20), RegistrationDeadline: day(-2),
			StartDate: day(3), EndDate: day(6), ValidSessionCount: 1, RegistrationCount: 4,
		}
	case enum.PeriodStatusCompleted:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(-40), RegistrationDeadline: day(-25),
			StartDate: day(-20), EndDate: day(-18), ValidSessionCount: 1, RegistrationCount: 4,
		}
	case enum.PeriodStatusCancelled:
		return exam.PeriodState{
			StoredStatus: status, RegistrationStart: day(-40), RegistrationDeadline: day(-25),
			StartDate: day(-20), EndDate: day(-18), ValidSessionCount: 1, RegistrationCount: 4,
		}
	}
	panic("periodStateFor: unhandled status " + string(status))
}

// sessionStateFor builds a SessionState whose effective status at fixedNow equals
// the requested status. The parent period is registration_open unless the case
// explicitly needs a different parent.
func sessionStateFor(status enum.ExamSessionStatus) exam.SessionState {
	openParent := struct {
		status        enum.ExamPeriodStatus
		regStart      *time.Time
		regDeadline   *time.Time
		periodEndDate *time.Time
	}{
		status:        enum.PeriodStatusRegistrationOpen,
		regStart:      day(-5),
		regDeadline:   day(5),
		periodEndDate: day(20),
	}

	base := exam.SessionState{
		Date:              day(10),
		Deadline:          day(5),
		Capacity:          intPtr(30),
		RegisteredCount:   10,
		PeriodStatus:      openParent.status,
		PeriodRegStart:    openParent.regStart,
		PeriodRegDeadline: openParent.regDeadline,
		PeriodEndDate:     openParent.periodEndDate,
	}

	switch status {
	case enum.SessionStatusDraft:
		base.StoredStatus = status
		return base
	case enum.SessionStatusScheduled:
		base.StoredStatus = status
		base.PublishAt = day(2)
		return base
	case enum.SessionStatusOpen:
		base.StoredStatus = status
		return base
	case enum.SessionStatusClosed:
		// Stored closed wins over the window so the reason survives. The exam day is
		// still ahead: once it passes the session becomes completed instead.
		base.StoredStatus = status
		base.ClosedReason = enum.ClosedReasonManual
		return base
	case enum.SessionStatusCompleted:
		// Exam day already passed relative to fixedNow.
		base.StoredStatus = enum.SessionStatusOpen
		base.Date = day(-5)
		base.Deadline = day(-10)
		return base
	case enum.SessionStatusCancelled:
		base.StoredStatus = status
		return base
	}
	panic("sessionStateFor: unhandled status " + string(status))
}

// assertEffectiveStatuses is a cheap sanity net: if the fixtures ever drift away
// from the status they claim to model, every matrix expectation after it would
// silently test the wrong cell. Fail loudly instead.
func assertEffectiveStatuses(t *testing.T) {
	t.Helper()
	for _, status := range allPeriodStatuses {
		if got := periodStateFor(status).Effective(fixedNow); got != status {
			t.Fatalf("period fixture for %q is effectively %q", status, got)
		}
	}
	for _, status := range allSessionStatuses {
		got, _ := sessionStateFor(status).Effective(fixedNow)
		if got != status {
			t.Fatalf("session fixture for %q is effectively %q", status, got)
		}
	}
}

// TestPeriodActionMatrix walks the full "Bảng hành động của đợt thi".
func TestPeriodActionMatrix(t *testing.T) {
	assertEffectiveStatuses(t)

	type row struct {
		action  exam.Action
		allowed map[enum.ExamPeriodStatus]bool
	}

	everyStatus := allowOnly(allPeriodStatuses...)

	rows := []row{
		// Xem chi tiết, sao chép mã — always.
		{exam.ActionView, everyStatus},
		{exam.ActionDuplicate, everyStatus},
		// Xuất danh sách thí sinh — from registration_open onwards.
		{exam.ActionExportRoster, allowOnly(
			enum.PeriodStatusRegistrationOpen,
			enum.PeriodStatusRegistrationClosed,
			enum.PeriodStatusCompleted,
			enum.PeriodStatusCancelled,
		)},
		// Chỉnh sửa — full for draft/published, description-only while open.
		{exam.ActionEdit, allowOnly(
			enum.PeriodStatusDraft,
			enum.PeriodStatusPublished,
			enum.PeriodStatusRegistrationOpen,
		)},
		// Công bố — draft only, and needs ≥1 valid child session.
		{exam.ActionPublish, allowOnly(enum.PeriodStatusDraft)},
		// Thu hồi công bố — published, or registration_open with zero registrations.
		{exam.ActionUnpublish, allowOnly(enum.PeriodStatusPublished)},
		// Đóng đăng ký sớm — registration_open only.
		{exam.ActionCloseRegistration, allowOnly(enum.PeriodStatusRegistrationOpen)},
		// Mở lại đăng ký — registration_closed only, before exam day.
		{exam.ActionReopenRegistration, allowOnly(enum.PeriodStatusRegistrationClosed)},
		// Dời lịch — published / registration_open / registration_closed.
		{exam.ActionReschedule, allowOnly(
			enum.PeriodStatusPublished,
			enum.PeriodStatusRegistrationOpen,
			enum.PeriodStatusRegistrationClosed,
		)},
		// Hủy đợt — same window as reschedule.
		{exam.ActionCancel, allowOnly(
			enum.PeriodStatusPublished,
			enum.PeriodStatusRegistrationOpen,
			enum.PeriodStatusRegistrationClosed,
		)},
		// Xóa bản nháp — draft only.
		{exam.ActionDeleteDraft, allowOnly(enum.PeriodStatusDraft)},
		// Hoàn tất — registration_closed only.
		{exam.ActionComplete, allowOnly(enum.PeriodStatusRegistrationClosed)},
	}

	for _, r := range rows {
		for _, status := range allPeriodStatuses {
			status := status
			t.Run(string(r.action)+"/"+string(status), func(t *testing.T) {
				st := periodStateFor(status)
				want := r.allowed[status]
				err := exam.GuardPeriodAction(r.action, st, fixedNow)
				if want && err != nil {
					t.Fatalf("GuardPeriodAction(%q) on %q = %v, want allowed", r.action, status, err)
				}
				if !want {
					assertTransitionDenied(t, err, r.action, string(status))
				}
			})
		}
	}
}

// TestPeriodUnpublishWhileOpenRequiresZeroRegistrations covers the ◐ cell of the
// period matrix ("chỉ khi 0 đăng ký").
func TestPeriodUnpublishWhileOpenRequiresZeroRegistrations(t *testing.T) {
	withRegistrations := periodStateFor(enum.PeriodStatusRegistrationOpen)
	withRegistrations.RegistrationCount = 3
	if err := exam.GuardPeriodAction(exam.ActionUnpublish, withRegistrations, fixedNow); err == nil {
		t.Fatal("unpublish with registrations must be denied")
	}

	withoutRegistrations := periodStateFor(enum.PeriodStatusRegistrationOpen)
	withoutRegistrations.RegistrationCount = 0
	if err := exam.GuardPeriodAction(exam.ActionUnpublish, withoutRegistrations, fixedNow); err != nil {
		t.Fatalf("unpublish without registrations = %v, want allowed", err)
	}
}

// TestPeriodPublishRequiresValidSession covers "✓ (cần ≥1 ca hợp lệ)".
func TestPeriodPublishRequiresValidSession(t *testing.T) {
	empty := periodStateFor(enum.PeriodStatusDraft)
	empty.ValidSessionCount = 0
	err := exam.GuardPeriodAction(exam.ActionPublish, empty, fixedNow)
	if err == nil {
		t.Fatal("publish without a valid session must be denied")
	}
	if !exam.IsInvalidStateTransition(err) {
		t.Fatalf("error = %T, want *InvalidStateTransitionError", err)
	}
}

// TestPeriodReopenRegistrationDeniedAfterExamDay covers "◐ trước ngày thi".
func TestPeriodReopenRegistrationDeniedAfterExamDay(t *testing.T) {
	past := periodStateFor(enum.PeriodStatusRegistrationClosed)
	past.StartDate = day(-1)
	past.EndDate = day(2)
	err := exam.GuardPeriodAction(exam.ActionReopenRegistration, past, fixedNow)
	assertTransitionDenied(t, err, exam.ActionReopenRegistration, string(enum.PeriodStatusRegistrationClosed))

	future := periodStateFor(enum.PeriodStatusRegistrationClosed)
	if err := exam.GuardPeriodAction(exam.ActionReopenRegistration, future, fixedNow); err != nil {
		t.Fatalf("reopen before exam day = %v, want allowed", err)
	}
}

// TestSessionActionMatrix walks the full "Bảng hành động của ca thi".
func TestSessionActionMatrix(t *testing.T) {
	assertEffectiveStatuses(t)

	type row struct {
		action  exam.Action
		allowed map[enum.ExamSessionStatus]bool
	}

	everyStatus := func(statuses ...enum.ExamSessionStatus) map[enum.ExamSessionStatus]bool {
		set := make(map[enum.ExamSessionStatus]bool, len(statuses))
		for _, s := range statuses {
			set[s] = true
		}
		return set
	}

	rows := []row{
		// Xem chi tiết, sao chép mã — always.
		{exam.ActionView, everyStatus(allSessionStatuses...)},
		// Chỉnh sửa — draft/scheduled/open fully, closed notes-only (denial lives in
		// CanEditSessionSchedule / CanEditSessionNotes), nothing for completed/cancelled.
		{exam.ActionEdit, everyStatus(
			enum.SessionStatusDraft,
			enum.SessionStatusScheduled,
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
		)},
		// Công khai ngay — draft and scheduled.
		{exam.ActionPublish, everyStatus(
			enum.SessionStatusDraft,
			enum.SessionStatusScheduled,
		)},
		// Lên lịch công khai — draft and scheduled (change the moment).
		{exam.ActionSchedulePublish, everyStatus(
			enum.SessionStatusDraft,
			enum.SessionStatusScheduled,
		)},
		// Thu hồi công khai — scheduled, or open with zero registrations.
		{exam.ActionUnpublish, everyStatus(enum.SessionStatusScheduled)},
		// Đóng đăng ký sớm — open only.
		{exam.ActionCloseRegistration, everyStatus(enum.SessionStatusOpen)},
		// Mở lại đăng ký — closed only, conditions enforced below and in the service.
		{exam.ActionReopenRegistration, everyStatus(enum.SessionStatusClosed)},
		// Nới / giảm sĩ số — open and closed.
		{exam.ActionIncreaseCapacity, everyStatus(
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
		)},
		{exam.ActionDecreaseCapacity, everyStatus(
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
		)},
		// Dời giờ, đổi phòng — scheduled, open, closed.
		{exam.ActionReschedule, everyStatus(
			enum.SessionStatusScheduled,
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
		)},
		// Danh sách thí sinh — open onwards.
		{exam.ActionViewRoster, everyStatus(
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
			enum.SessionStatusCompleted,
			enum.SessionStatusCancelled,
		)},
		// Điểm danh — closed on exam day, plus completed.
		{exam.ActionCheckIn, everyStatus(enum.SessionStatusCompleted)},
		// Hủy ca thi — open and closed only (draft/scheduled use delete-draft).
		{exam.ActionCancel, everyStatus(
			enum.SessionStatusOpen,
			enum.SessionStatusClosed,
		)},
		// Xóa bản nháp — draft and scheduled.
		{exam.ActionDeleteDraft, everyStatus(
			enum.SessionStatusDraft,
			enum.SessionStatusScheduled,
		)},
		// Nhân bản ca thi — removed from the product, so it is denied everywhere.
		{exam.ActionDuplicate, everyStatus()},
	}

	for _, r := range rows {
		for _, status := range allSessionStatuses {
			status := status
			t.Run(string(r.action)+"/"+string(status), func(t *testing.T) {
				st := sessionStateFor(status)
				want := r.allowed[status]
				err := exam.GuardSessionAction(r.action, st, fixedNow)
				if want && err != nil {
					t.Fatalf("GuardSessionAction(%q) on %q = %v, want allowed", r.action, status, err)
				}
				if !want {
					assertTransitionDenied(t, err, r.action, string(status))
				}
			})
		}
	}
}

// TestSessionUnpublishWhileOpenRequiresZeroRegistrations covers the ◐ cell of
// the session matrix.
func TestSessionUnpublishWhileOpenRequiresZeroRegistrations(t *testing.T) {
	withRegistrations := sessionStateFor(enum.SessionStatusOpen)
	withRegistrations.RegisteredCount = 2
	if err := exam.GuardSessionAction(exam.ActionUnpublish, withRegistrations, fixedNow); err == nil {
		t.Fatal("unpublish with registrations must be denied")
	}

	withoutRegistrations := sessionStateFor(enum.SessionStatusOpen)
	withoutRegistrations.RegisteredCount = 0
	if err := exam.GuardSessionAction(exam.ActionUnpublish, withoutRegistrations, fixedNow); err != nil {
		t.Fatalf("unpublish without registrations = %v, want allowed", err)
	}
}

// TestSessionCheckInOnlyOnExamDay covers "\u25d0 ch\u1ec9 trong ng\u00e0y thi".
func TestSessionCheckInOnlyOnExamDay(t *testing.T) {
	t.Run("before exam day is denied", func(t *testing.T) {
		closed := sessionStateFor(enum.SessionStatusClosed)
		closed.Date = day(3)
		if err := exam.GuardSessionAction(exam.ActionCheckIn, closed, fixedNow); err == nil {
			t.Fatal("check-in before exam day must be denied")
		}
	})

	t.Run("on exam day is allowed", func(t *testing.T) {
		closed := sessionStateFor(enum.SessionStatusClosed)
		closed.Date = timePtr(fixedNow)
		if err := exam.GuardSessionAction(exam.ActionCheckIn, closed, fixedNow); err != nil {
			t.Fatalf("check-in on exam day = %v, want allowed", err)
		}
	})

	t.Run("after exam day the session is completed and stays checkable", func(t *testing.T) {
		closed := sessionStateFor(enum.SessionStatusClosed)
		closed.Date = day(-3)

		if eff, _ := closed.Effective(fixedNow); eff != enum.SessionStatusCompleted {
			t.Fatalf("effective = %q, want completed", eff)
		}
		if err := exam.GuardSessionAction(exam.ActionCheckIn, closed, fixedNow); err != nil {
			t.Fatalf("check-in on a completed session = %v, want allowed", err)
		}
	})
}

// TestSessionReopenRegistrationDecidedByClosedReason locks the four closed_reason
// reopen rules from the spec.
func TestSessionReopenRegistrationDecidedByClosedReason(t *testing.T) {
	cases := []struct {
		name       string
		reason     enum.ExamSessionClosedReason
		mutate     func(*exam.SessionState)
		wantAllow  bool
		wantReason string
	}{
		{
			name:      "manual reopens immediately",
			reason:    enum.ClosedReasonManual,
			wantAllow: true,
		},
		{
			name:       "full needs a capacity increase first",
			reason:     enum.ClosedReasonFull,
			wantAllow:  false,
			wantReason: "sĩ số",
		},
		{
			name:      "deadline reopens (new deadline enforced by the service)",
			reason:    enum.ClosedReasonDeadline,
			wantAllow: true,
		},
		{
			name:       "period_closed can only be reopened from the period",
			reason:     enum.ClosedReasonPeriodClosed,
			wantAllow:  false,
			wantReason: "đợt thi",
		},
		{
			name:   "parent period must be registration_open",
			reason: enum.ClosedReasonManual,
			mutate: func(s *exam.SessionState) {
				s.PeriodStatus = enum.PeriodStatusPublished
				s.PeriodRegStart = day(5)
				s.PeriodRegDeadline = day(10)
			},
			wantAllow:  false,
			wantReason: "Đợt thi cha",
		},
		{
			// Once the exam day passes the session stops being `closed` altogether, so
			// the reopen action is rejected by the generic closed-state rule.
			name:   "exam day already passed",
			reason: enum.ClosedReasonManual,
			mutate: func(s *exam.SessionState) {
				s.Date = day(-1)
			},
			wantAllow:  false,
			wantReason: "đã đóng",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := sessionStateFor(enum.SessionStatusClosed)
			st.ClosedReason = tc.reason
			if tc.mutate != nil {
				tc.mutate(&st)
			}

			err := exam.GuardSessionAction(exam.ActionReopenRegistration, st, fixedNow)
			if tc.wantAllow {
				if err != nil {
					t.Fatalf("reopen (%s) = %v, want allowed", tc.reason, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("reopen (%s) allowed, want denied", tc.reason)
			}
			if !strings.Contains(err.Error(), tc.wantReason) {
				t.Fatalf("reason = %q, want it to mention %q", err.Error(), tc.wantReason)
			}
		})
	}
}

// TestAvailableActionsNeverOfferDuplicateSessions guards the removal of the
// duplicate-session feature: the action must not be advertised by any state.
func TestAvailableActionsNeverOfferDuplicateSessions(t *testing.T) {
	for _, status := range allSessionStatuses {
		actions := exam.AvailableSessionActions(sessionStateFor(status), fixedNow)
		for _, a := range actions {
			if a == exam.ActionDuplicate {
				t.Fatalf("status %q still advertises ActionDuplicate", status)
			}
		}
	}

	for _, status := range allPeriodStatuses {
		for _, a := range exam.AvailablePeriodActions(periodStateFor(status), fixedNow) {
			if a != exam.ActionDuplicate {
				continue
			}
			found := false
			for _, candidate := range allPeriodStatuses {
				if candidate == status {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("period status %q advertised ActionDuplicate", status)
			}
		}
	}
}

// TestEffectiveStatusDriftEdgeCases covers the cases where the stored column lags
// behind wall-clock time — the reason rule #1 of the spec exists.
func TestEffectiveStatusDriftEdgeCases(t *testing.T) {
	t.Run("stored scheduled with elapsed publish_at still waits for the parent", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusScheduled)
		st.PublishAt = day(-1)
		// Parent still `published` (registration has not started) → stay scheduled.
		st.PeriodStatus = enum.PeriodStatusPublished
		st.PeriodRegStart = day(5)
		st.PeriodRegDeadline = day(10)

		got, _ := st.Effective(fixedNow)
		if got != enum.SessionStatusScheduled {
			t.Fatalf("effective = %q, want scheduled", got)
		}
	})

	t.Run("stored scheduled with elapsed publish_at opens once the parent is open", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusScheduled)
		st.PublishAt = day(-1)

		got, _ := st.Effective(fixedNow)
		if got != enum.SessionStatusOpen {
			t.Fatalf("effective = %q, want open", got)
		}
	})

	t.Run("stored open closes when the parent period closes", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.PeriodStatus = enum.PeriodStatusRegistrationClosed

		got, reason := st.Effective(fixedNow)
		if got != enum.SessionStatusClosed || reason != enum.ClosedReasonPeriodClosed {
			t.Fatalf("effective = %q/%q, want closed/period_closed", got, reason)
		}
	})

	t.Run("stored open closes when the deadline elapses", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.Deadline = day(-1)

		got, reason := st.Effective(fixedNow)
		if got != enum.SessionStatusClosed || reason != enum.ClosedReasonDeadline {
			t.Fatalf("effective = %q/%q, want closed/deadline", got, reason)
		}
	})

	t.Run("stored open closes when capacity is reached", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.Capacity = intPtr(10)
		st.RegisteredCount = 10

		got, reason := st.Effective(fixedNow)
		if got != enum.SessionStatusClosed || reason != enum.ClosedReasonFull {
			t.Fatalf("effective = %q/%q, want closed/full", got, reason)
		}
	})

	t.Run("stored closed without a reason defaults to manual", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusClosed)
		st.ClosedReason = ""

		got, reason := st.Effective(fixedNow)
		if got != enum.SessionStatusClosed || reason != enum.ClosedReasonManual {
			t.Fatalf("effective = %q/%q, want closed/manual", got, reason)
		}
	})

	t.Run("cancelled parent propagates to the session", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.PeriodStatus = enum.PeriodStatusCancelled

		got, _ := st.Effective(fixedNow)
		if got != enum.SessionStatusCancelled {
			t.Fatalf("effective = %q, want cancelled", got)
		}
	})

	t.Run("completed parent propagates to the session", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.PeriodStatus = enum.PeriodStatusCompleted

		got, _ := st.Effective(fixedNow)
		if got != enum.SessionStatusCompleted {
			t.Fatalf("effective = %q, want completed", got)
		}
	})

	t.Run("exam day passed makes the session completed even while stored open", func(t *testing.T) {
		st := sessionStateFor(enum.SessionStatusOpen)
		st.Date = day(-1)

		got, _ := st.Effective(fixedNow)
		if got != enum.SessionStatusCompleted {
			t.Fatalf("effective = %q, want completed", got)
		}
	})
}

// TestEffectivePeriodStatusDriftEdgeCases covers the period side of rule #1.
func TestEffectivePeriodStatusDriftEdgeCases(t *testing.T) {
	t.Run("stored published before registration start stays published", func(t *testing.T) {
		got := exam.EffectivePeriodStatus(fixedNow, enum.PeriodStatusPublished, day(3), day(8))
		if got != enum.PeriodStatusPublished {
			t.Fatalf("effective = %q, want published", got)
		}
	})

	t.Run("stored published inside the window becomes registration_open", func(t *testing.T) {
		got := exam.EffectivePeriodStatus(fixedNow, enum.PeriodStatusPublished, day(-3), day(8))
		if got != enum.PeriodStatusRegistrationOpen {
			t.Fatalf("effective = %q, want registration_open", got)
		}
	})

	t.Run("stored registration_open past the deadline becomes registration_closed", func(t *testing.T) {
		got := exam.EffectivePeriodStatus(fixedNow, enum.PeriodStatusRegistrationOpen, day(-10), day(-1))
		if got != enum.PeriodStatusRegistrationClosed {
			t.Fatalf("effective = %q, want registration_closed", got)
		}
	})

	t.Run("stored registration_closed never reopens by time alone", func(t *testing.T) {
		got := exam.EffectivePeriodStatus(fixedNow, enum.PeriodStatusRegistrationClosed, day(-1), day(10))
		if got != enum.PeriodStatusRegistrationClosed {
			t.Fatalf("effective = %q, want registration_closed", got)
		}
	})

	t.Run("draft and cancelled are sticky", func(t *testing.T) {
		for _, status := range []enum.ExamPeriodStatus{enum.PeriodStatusDraft, enum.PeriodStatusCancelled, enum.PeriodStatusCompleted} {
			if got := exam.EffectivePeriodStatus(fixedNow, status, day(-3), day(8)); got != status {
				t.Fatalf("%q effective = %q, want %q", status, got, status)
			}
		}
	})
}

// assertTransitionDenied asserts the guard returned the documented 409-shaped error.
func assertTransitionDenied(t *testing.T, err error, action exam.Action, status string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%q on %q was allowed, want denied", action, status)
	}
	var transition *exam.InvalidStateTransitionError
	if !errors.As(err, &transition) {
		t.Fatalf("%q on %q error = %T, want *InvalidStateTransitionError", action, status, err)
	}
	if transition.Reason == "" {
		t.Fatalf("%q on %q denial has an empty reason; the 409 body would be blank", action, status)
	}
	if transition.Action != action {
		t.Fatalf("%q on %q denial action = %q, want %q", action, status, transition.Action, action)
	}
}

func TestSessionPublishDeniedWhenParentPeriodIsDraft(t *testing.T) {
	st := exam.SessionState{
		StoredStatus: enum.SessionStatusDraft,
		Date:         day(10),
		PeriodStatus: enum.PeriodStatusDraft,
	}

	actions := exam.AvailableSessionActions(st, fixedNow)
	for _, a := range actions {
		if a == exam.ActionPublish || a == exam.ActionSchedulePublish {
			t.Fatalf("parent period in draft should not allow action %q in available_actions", a)
		}
	}

	err := exam.GuardSessionAction(exam.ActionPublish, st, fixedNow)
	assertTransitionDenied(t, err, exam.ActionPublish, string(enum.SessionStatusDraft))
	var transErr *exam.InvalidStateTransitionError
	if errors.As(err, &transErr) {
		wantReason := "Đợt thi đang ở trạng thái nháp; phải công bố đợt thi trước khi công khai ca thi"
		if transErr.Reason != wantReason {
			t.Fatalf("reason = %q, want %q", transErr.Reason, wantReason)
		}
	}

	errSched := exam.GuardSessionAction(exam.ActionSchedulePublish, st, fixedNow)
	assertTransitionDenied(t, errSched, exam.ActionSchedulePublish, string(enum.SessionStatusDraft))

	// When parent period is published, publish and schedule_publish should be available
	st.PeriodStatus = enum.PeriodStatusPublished
	actionsPublished := exam.AvailableSessionActions(st, fixedNow)
	hasPublish := false
	hasSchedule := false
	for _, a := range actionsPublished {
		if a == exam.ActionPublish {
			hasPublish = true
		}
		if a == exam.ActionSchedulePublish {
			hasSchedule = true
		}
	}
	if !hasPublish || !hasSchedule {
		t.Fatalf("parent period in published should offer publish and schedule_publish, got %v", actionsPublished)
	}
}

