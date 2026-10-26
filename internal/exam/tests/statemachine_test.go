package tests

import (
	"errors"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/exam/enum"
)

func TestEffectivePeriodStatus(t *testing.T) {
	now := fixedNow
	future := day(10)
	past := day(-10)

	cases := []struct {
		name     string
		stored   enum.ExamPeriodStatus
		regStart *time.Time
		deadline *time.Time
		want     enum.ExamPeriodStatus
	}{
		{name: "draft is sticky", stored: enum.PeriodStatusDraft, regStart: past, deadline: future, want: enum.PeriodStatusDraft},
		{name: "cancelled is sticky", stored: enum.PeriodStatusCancelled, want: enum.PeriodStatusCancelled},
		{name: "completed is sticky", stored: enum.PeriodStatusCompleted, want: enum.PeriodStatusCompleted},
		{name: "stored registration_closed is sticky", stored: enum.PeriodStatusRegistrationClosed, regStart: past, deadline: future, want: enum.PeriodStatusRegistrationClosed},
		{name: "stored published before start stays published", stored: enum.PeriodStatusPublished, regStart: future, deadline: future, want: enum.PeriodStatusPublished},
		{name: "stored published inside window is open", stored: enum.PeriodStatusPublished, regStart: past, deadline: future, want: enum.PeriodStatusRegistrationOpen},
		{name: "deadline reached overrides stored published", stored: enum.PeriodStatusPublished, regStart: past, deadline: past, want: enum.PeriodStatusRegistrationClosed},
		{name: "no window means open", stored: enum.PeriodStatusRegistrationOpen, want: enum.PeriodStatusRegistrationOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := exam.EffectivePeriodStatus(now, tc.stored, tc.regStart, tc.deadline)
			if got != tc.want {
				t.Fatalf("EffectivePeriodStatus() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSessionEffectiveStatus(t *testing.T) {
	now := fixedNow
	future := day(5)
	past := day(-1)
	nextWeek := day(7)
	tomorrow := day(1)
	capacity := 10

	cases := []struct {
		name       string
		state      exam.SessionState
		wantStatus enum.ExamSessionStatus
		wantReason enum.ExamSessionClosedReason
	}{
		{
			name:       "draft",
			state:      exam.SessionState{StoredStatus: enum.SessionStatusDraft, Date: future},
			wantStatus: enum.SessionStatusDraft,
		},
		{
			name:       "cancelled",
			state:      exam.SessionState{StoredStatus: enum.SessionStatusCancelled},
			wantStatus: enum.SessionStatusCancelled,
		},
		{
			name:       "completed",
			state:      exam.SessionState{StoredStatus: enum.SessionStatusCompleted},
			wantStatus: enum.SessionStatusCompleted,
		},
		{
			name:       "exam day passed promotes to completed",
			state:      exam.SessionState{StoredStatus: enum.SessionStatusOpen, Date: past, Deadline: future},
			wantStatus: enum.SessionStatusCompleted,
		},
		{
			name: "scheduled without publish_at",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusScheduled, Date: future,
			},
			wantStatus: enum.SessionStatusScheduled,
		},
		{
			name: "scheduled with future publish_at",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusScheduled, Date: future, PublishAt: nextWeek,
			},
			wantStatus: enum.SessionStatusScheduled,
		},
		{
			name: "scheduled whose publish_at elapsed becomes open",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusScheduled, Date: future, PublishAt: past, Deadline: future,
			},
			wantStatus: enum.SessionStatusOpen,
		},
		{
			name: "open but deadline elapsed",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: past,
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonDeadline,
		},
		{
			name: "open but capacity reached",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: future,
				Capacity: &capacity, RegisteredCount: capacity,
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonFull,
		},
		{
			name: "open with free seats",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: future,
				Capacity: &capacity, RegisteredCount: capacity - 1,
			},
			wantStatus: enum.SessionStatusOpen,
		},
		{
			name: "stored closed keeps manual reason",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusClosed, ClosedReason: enum.ClosedReasonManual, Date: future,
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonManual,
		},
		{
			name: "stored closed without reason defaults to manual",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusClosed, Date: future,
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonManual,
		},
		{
			name: "open session under a registration_closed period is period_closed",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: future,
				PeriodStatus: enum.PeriodStatusRegistrationClosed,
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonPeriodClosed,
		},
		{
			name: "open session under a cancelled period is cancelled",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: future,
				PeriodStatus: enum.PeriodStatusCancelled,
			},
			wantStatus: enum.SessionStatusCancelled,
		},
		{
			name: "open session under a lagging published period is period_closed",
			state: exam.SessionState{
				StoredStatus: enum.SessionStatusOpen, Date: future, Deadline: future,
				PeriodStatus:   enum.PeriodStatusPublished,
				PeriodRegStart: day(5),
			},
			wantStatus: enum.SessionStatusClosed,
			wantReason: enum.ClosedReasonPeriodClosed,
		},
		{
			name: "scheduled with elapsed publish_at waits for the period to open",
			state: exam.SessionState{
				StoredStatus:   enum.SessionStatusScheduled,
				PublishAt:      past,
				Date:           future,
				Deadline:       future,
				PeriodStatus:   enum.PeriodStatusPublished,
				PeriodRegStart: day(5),
			},
			wantStatus: enum.SessionStatusScheduled,
		},
		{
			name: "scheduled with elapsed publish_at goes open once the period opened",
			state: exam.SessionState{
				StoredStatus:      enum.SessionStatusScheduled,
				PublishAt:         past,
				Date:              future,
				Deadline:          future,
				PeriodStatus:      enum.PeriodStatusRegistrationOpen,
				PeriodRegStart:    past,
				PeriodRegDeadline: future,
				PeriodEndDate:     future,
			},
			wantStatus: enum.SessionStatusOpen,
		},
		{
			name: "open session under an effectively open period stays open",
			state: exam.SessionState{
				StoredStatus:      enum.SessionStatusOpen,
				Date:              tomorrow,
				Deadline:          future,
				PeriodStatus:      enum.PeriodStatusPublished,
				PeriodRegStart:    past,
				PeriodRegDeadline: future,
				PeriodEndDate:     future,
				Capacity:          &capacity,
				RegisteredCount:   1,
			},
			wantStatus: enum.SessionStatusOpen,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotReason := tc.state.Effective(now)
			if gotStatus != tc.wantStatus || gotReason != tc.wantReason {
				t.Fatalf("Effective() = (%s, %q), want (%s, %q)",
					gotStatus, gotReason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

func TestGuardPeriodActionDeniesIllegalTransitions(t *testing.T) {
	draft := exam.PeriodState{StoredStatus: enum.PeriodStatusDraft, ValidSessionCount: 2}
	emptyDraft := exam.PeriodState{StoredStatus: enum.PeriodStatusDraft, ValidSessionCount: 0}
	published := exam.PeriodState{StoredStatus: enum.PeriodStatusPublished, RegistrationStart: day(5), RegistrationDeadline: day(10)}
	open := exam.PeriodState{
		StoredStatus:         enum.PeriodStatusPublished,
		RegistrationStart:    day(-1),
		RegistrationDeadline: day(10),
		EndDate:              day(20),
	}
	openWithRegs := open
	openWithRegs.RegistrationCount = 3
	closed := exam.PeriodState{
		StoredStatus: enum.PeriodStatusRegistrationClosed,
		StartDate:    day(5),
		EndDate:      day(20),
	}
	completed := exam.PeriodState{StoredStatus: enum.PeriodStatusCompleted}
	cancelled := exam.PeriodState{StoredStatus: enum.PeriodStatusCancelled}

	cases := []struct {
		name    string
		action  exam.Action
		state   exam.PeriodState
		allowed bool
	}{
		{name: "view any state", action: exam.ActionView, state: cancelled, allowed: true},
		{name: "duplicate any state", action: exam.ActionDuplicate, state: completed, allowed: true},
		{name: "export draft denied", action: exam.ActionExportRoster, state: draft, allowed: false},
		{name: "export open allowed", action: exam.ActionExportRoster, state: open, allowed: true},
		{name: "edit draft allowed", action: exam.ActionEdit, state: draft, allowed: true},
		{name: "edit open allowed", action: exam.ActionEdit, state: open, allowed: true},
		{name: "edit closed denied", action: exam.ActionEdit, state: closed, allowed: false},
		{name: "edit cancelled denied", action: exam.ActionEdit, state: cancelled, allowed: false},
		{name: "publish draft with sessions", action: exam.ActionPublish, state: draft, allowed: true},
		{name: "publish draft without sessions denied", action: exam.ActionPublish, state: emptyDraft, allowed: false},
		{name: "publish published denied", action: exam.ActionPublish, state: published, allowed: false},
		{name: "unpublish published allowed", action: exam.ActionUnpublish, state: published, allowed: true},
		{name: "unpublish open without registrations allowed", action: exam.ActionUnpublish, state: open, allowed: true},
		{name: "unpublish open with registrations denied", action: exam.ActionUnpublish, state: openWithRegs, allowed: false},
		{name: "close registration open allowed", action: exam.ActionCloseRegistration, state: open, allowed: true},
		{name: "close registration draft denied", action: exam.ActionCloseRegistration, state: draft, allowed: false},
		{name: "reopen registration closed allowed", action: exam.ActionReopenRegistration, state: closed, allowed: true},
		{name: "reopen registration open denied", action: exam.ActionReopenRegistration, state: open, allowed: false},
		{name: "reschedule published allowed", action: exam.ActionReschedule, state: published, allowed: true},
		{name: "reschedule draft denied", action: exam.ActionReschedule, state: draft, allowed: false},
		{name: "cancel published allowed", action: exam.ActionCancel, state: published, allowed: true},
		{name: "cancel draft denied", action: exam.ActionCancel, state: draft, allowed: false},
		{name: "cancel completed denied", action: exam.ActionCancel, state: completed, allowed: false},
		{name: "delete draft allowed", action: exam.ActionDeleteDraft, state: draft, allowed: true},
		{name: "delete published denied", action: exam.ActionDeleteDraft, state: published, allowed: false},
		{name: "complete closed allowed", action: exam.ActionComplete, state: closed, allowed: true},
		{name: "complete open denied", action: exam.ActionComplete, state: open, allowed: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := exam.GuardPeriodAction(tc.action, tc.state, fixedNow)
			if tc.allowed && err != nil {
				t.Fatalf("expected allowed, got %v", err)
			}
			if !tc.allowed {
				if err == nil {
					t.Fatalf("expected denial, got nil")
				}
				var te *exam.InvalidStateTransitionError
				if !errors.As(err, &te) {
					t.Fatalf("expected *InvalidStateTransitionError, got %T", err)
				}
				if te.Reason == "" {
					t.Fatal("denial must carry a reason")
				}
				if te.Action != tc.action {
					t.Fatalf("error action = %s, want %s", te.Action, tc.action)
				}
			}
		})
	}
}

func TestGuardSessionActionDeniesIllegalTransitions(t *testing.T) {
	future := day(5)
	deadline := day(3)
	capacity := 10
	now := fixedNow

	draftIncomplete := exam.SessionState{StoredStatus: enum.SessionStatusDraft}
	draftReady := exam.SessionState{
		StoredStatus: enum.SessionStatusDraft,
		Date:         future,
		Capacity:     &capacity,
		Deadline:     deadline,
	}
	scheduled := exam.SessionState{
		StoredStatus: enum.SessionStatusScheduled,
		Date:         future,
		PublishAt:    future,
		Deadline:     deadline,
		Capacity:     &capacity,
	}
	openNoRegs := exam.SessionState{
		StoredStatus: enum.SessionStatusOpen,
		Date:         future,
		Deadline:     deadline,
		Capacity:     &capacity,
	}
	openWithRegs := openNoRegs
	openWithRegs.RegisteredCount = 2
	closedManual := exam.SessionState{
		StoredStatus: enum.SessionStatusClosed,
		ClosedReason: enum.ClosedReasonManual,
		Date:         future,
		Deadline:     deadline,
		Capacity:     &capacity,
	}
	closedFull := closedManual
	closedFull.ClosedReason = enum.ClosedReasonFull
	closedDeadline := closedManual
	closedDeadline.ClosedReason = enum.ClosedReasonDeadline
	closedPeriod := closedManual
	closedPeriod.ClosedReason = enum.ClosedReasonPeriodClosed
	completed := exam.SessionState{StoredStatus: enum.SessionStatusCompleted, Date: future}
	cancelled := exam.SessionState{StoredStatus: enum.SessionStatusCancelled, Date: future}

	cases := []struct {
		name    string
		action  exam.Action
		state   exam.SessionState
		allowed bool
	}{
		{name: "view cancelled", action: exam.ActionView, state: cancelled, allowed: true},
		{name: "duplicate completed", action: exam.ActionDuplicate, state: completed, allowed: true},
		{name: "edit draft", action: exam.ActionEdit, state: draftIncomplete, allowed: true},
		{name: "edit open", action: exam.ActionEdit, state: openWithRegs, allowed: true},
		{name: "edit closed", action: exam.ActionEdit, state: closedManual, allowed: true},
		{name: "edit completed denied", action: exam.ActionEdit, state: completed, allowed: false},
		{name: "edit cancelled denied", action: exam.ActionEdit, state: cancelled, allowed: false},
		{name: "publish draft", action: exam.ActionPublish, state: draftReady, allowed: true},
		{name: "publish scheduled", action: exam.ActionPublish, state: scheduled, allowed: true},
		{name: "publish open denied", action: exam.ActionPublish, state: openNoRegs, allowed: false},
		{name: "schedule publish draft", action: exam.ActionSchedulePublish, state: draftIncomplete, allowed: true},
		{name: "unpublish scheduled", action: exam.ActionUnpublish, state: scheduled, allowed: true},
		{name: "unpublish open without registrations", action: exam.ActionUnpublish, state: openNoRegs, allowed: true},
		{name: "unpublish open with registrations denied", action: exam.ActionUnpublish, state: openWithRegs, allowed: false},
		{name: "close open", action: exam.ActionCloseRegistration, state: openNoRegs, allowed: true},
		{name: "close draft denied", action: exam.ActionCloseRegistration, state: draftReady, allowed: false},
		{name: "reopen manual", action: exam.ActionReopenRegistration, state: closedManual, allowed: true},
		{name: "reopen full denied", action: exam.ActionReopenRegistration, state: closedFull, allowed: false},
		{name: "reopen deadline allowed once a new deadline is supplied", action: exam.ActionReopenRegistration, state: closedDeadline, allowed: true},
		{name: "reopen period_closed denied", action: exam.ActionReopenRegistration, state: closedPeriod, allowed: false},
		{name: "increase capacity open", action: exam.ActionIncreaseCapacity, state: openNoRegs, allowed: true},
		{name: "increase capacity closed", action: exam.ActionIncreaseCapacity, state: closedFull, allowed: true},
		{name: "increase capacity completed denied", action: exam.ActionIncreaseCapacity, state: completed, allowed: false},
		{name: "decrease capacity open", action: exam.ActionDecreaseCapacity, state: openWithRegs, allowed: true},
		{name: "reschedule scheduled", action: exam.ActionReschedule, state: scheduled, allowed: true},
		{name: "reschedule draft denied", action: exam.ActionReschedule, state: draftReady, allowed: false},
		{name: "roster draft denied", action: exam.ActionViewRoster, state: draftReady, allowed: false},
		{name: "roster closed allowed", action: exam.ActionViewRoster, state: closedManual, allowed: true},
		{name: "check-in closed off exam day denied", action: exam.ActionCheckIn, state: closedManual, allowed: false},
		{name: "cancel open", action: exam.ActionCancel, state: openNoRegs, allowed: true},
		{name: "cancel closed", action: exam.ActionCancel, state: closedManual, allowed: true},
		{name: "cancel draft denied", action: exam.ActionCancel, state: draftReady, allowed: false},
		{name: "cancel scheduled denied", action: exam.ActionCancel, state: scheduled, allowed: false},
		{name: "delete draft", action: exam.ActionDeleteDraft, state: draftReady, allowed: true},
		{name: "delete scheduled", action: exam.ActionDeleteDraft, state: scheduled, allowed: true},
		{name: "delete open denied", action: exam.ActionDeleteDraft, state: openNoRegs, allowed: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := exam.GuardSessionAction(tc.action, tc.state, now)
			if tc.allowed && err != nil {
				t.Fatalf("expected allowed, got %v", err)
			}
			if !tc.allowed {
				if err == nil {
					t.Fatalf("expected denial, got nil")
				}
				var te *exam.InvalidStateTransitionError
				if !errors.As(err, &te) {
					t.Fatalf("expected *InvalidStateTransitionError, got %T", err)
				}
				if te.Reason == "" {
					t.Fatal("denial must carry a reason")
				}
			}
		})
	}
}

func TestReopenSessionRegistrationRequiresOpenParentPeriod(t *testing.T) {
	closedManual := exam.SessionState{
		StoredStatus: enum.SessionStatusClosed,
		ClosedReason: enum.ClosedReasonManual,
		Date:         day(5),
		PeriodStatus: enum.PeriodStatusRegistrationClosed,
	}
	err := exam.GuardSessionAction(exam.ActionReopenRegistration, closedManual, fixedNow)
	if err == nil {
		t.Fatal("expected denial when parent period is not registration_open")
	}

	closedManual.PeriodStatus = enum.PeriodStatusRegistrationOpen
	if err := exam.GuardSessionAction(exam.ActionReopenRegistration, closedManual, fixedNow); err != nil {
		t.Fatalf("expected reopen to be allowed, got %v", err)
	}
}

func TestCheckInOnlyOnExamDay(t *testing.T) {
	state := exam.SessionState{
		StoredStatus: enum.SessionStatusClosed,
		ClosedReason: enum.ClosedReasonManual,
		Date:         &fixedNow,
	}
	if err := exam.GuardSessionAction(exam.ActionCheckIn, state, fixedNow); err != nil {
		t.Fatalf("expected check-in to be allowed on exam day, got %v", err)
	}
	state.Date = day(2)
	if err := exam.GuardSessionAction(exam.ActionCheckIn, state, fixedNow); err == nil {
		t.Fatal("expected check-in to be denied outside exam day")
	}
}

func TestAvailableActionsNeverContradictsGuard(t *testing.T) {
	states := []exam.SessionState{
		{StoredStatus: enum.SessionStatusDraft, Date: day(5)},
		{StoredStatus: enum.SessionStatusScheduled, Date: day(5), PublishAt: day(1)},
		{StoredStatus: enum.SessionStatusOpen, Date: day(5), Deadline: day(1)},
		{StoredStatus: enum.SessionStatusClosed, ClosedReason: enum.ClosedReasonManual, Date: day(5)},
		{StoredStatus: enum.SessionStatusCompleted},
		{StoredStatus: enum.SessionStatusCancelled},
	}
	for _, st := range states {
		eff, _ := st.Effective(fixedNow)
		for _, a := range exam.AvailableSessionActions(st, fixedNow) {
			if err := exam.GuardSessionAction(a, st, fixedNow); err != nil {
				t.Fatalf("action %s reported available for %s but guard denied: %v", a, eff, err)
			}
		}
	}

	periods := []exam.PeriodState{
		{StoredStatus: enum.PeriodStatusDraft, ValidSessionCount: 1},
		{StoredStatus: enum.PeriodStatusPublished},
		{StoredStatus: enum.PeriodStatusRegistrationOpen, EndDate: day(20)},
		{StoredStatus: enum.PeriodStatusRegistrationClosed, StartDate: day(10), EndDate: day(20)},
		{StoredStatus: enum.PeriodStatusCompleted},
		{StoredStatus: enum.PeriodStatusCancelled},
	}
	for _, st := range periods {
		eff := st.Effective(fixedNow)
		for _, a := range exam.AvailablePeriodActions(st, fixedNow) {
			if err := exam.GuardPeriodAction(a, st, fixedNow); err != nil {
				t.Fatalf("action %s reported available for %s but guard denied: %v", a, eff, err)
			}
		}
	}
}

func TestCanEditSessionSchedule(t *testing.T) {
	future := day(5)
	cases := []struct {
		name  string
		state exam.SessionState
		want  bool
	}{
		{name: "draft", state: exam.SessionState{StoredStatus: enum.SessionStatusDraft, Date: future}, want: true},
		{name: "scheduled", state: exam.SessionState{StoredStatus: enum.SessionStatusScheduled, Date: future, PublishAt: future}, want: true},
		{name: "open without registrations", state: exam.SessionState{StoredStatus: enum.SessionStatusOpen, Date: future}, want: true},
		{name: "open with registrations", state: exam.SessionState{StoredStatus: enum.SessionStatusOpen, Date: future, RegisteredCount: 1}, want: false},
		{name: "closed", state: exam.SessionState{StoredStatus: enum.SessionStatusClosed, ClosedReason: enum.ClosedReasonManual, Date: future}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.CanEditSessionSchedule(fixedNow); got != tc.want {
				t.Fatalf("CanEditSessionSchedule() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanEditSessionNotes(t *testing.T) {
	future := day(5)
	for _, st := range []exam.SessionState{
		{StoredStatus: enum.SessionStatusDraft, Date: future},
		{StoredStatus: enum.SessionStatusOpen, Date: future, RegisteredCount: 3},
		{StoredStatus: enum.SessionStatusClosed, ClosedReason: enum.ClosedReasonManual, Date: future},
	} {
		if !st.CanEditSessionNotes(fixedNow) {
			t.Fatalf("expected notes editable for %+v", st)
		}
	}
	completed := exam.SessionState{StoredStatus: enum.SessionStatusCompleted, Date: future}
	if completed.CanEditSessionNotes(fixedNow) {
		t.Fatal("expected notes to be locked for completed session")
	}
}
