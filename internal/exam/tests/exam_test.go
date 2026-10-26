package tests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/exam/enum"
)

// fakeAuditService satisfies exam's audit dependency for service tests.
type fakeAuditService struct{}

func (fakeAuditService) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return nil
}

func (fakeAuditService) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func TestExamShiftValid(t *testing.T) {
	cases := []struct {
		name  string
		value enum.ExamShift
		valid bool
	}{
		{name: "morning", value: enum.ShiftMorning, valid: true},
		{name: "afternoon", value: enum.ShiftAfternoon, valid: true},
		{name: "evening", value: enum.ShiftEvening, valid: true},
		{name: "empty", value: "", valid: false},
		{name: "unknown", value: "night", valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.Valid(); got != tc.valid {
				t.Fatalf("%s.Valid() = %v, want %v", tc.value, got, tc.valid)
			}
			if tc.name != "empty" && tc.value.String() != string(tc.value) {
				t.Fatalf("String() mismatch")
			}
		})
	}
}

func TestExamSessionStatusValid(t *testing.T) {
	cases := []struct {
		name  string
		value enum.ExamSessionStatus
		valid bool
	}{
		{name: "draft", value: enum.SessionStatusDraft, valid: true},
		{name: "scheduled", value: enum.SessionStatusScheduled, valid: true},
		{name: "open", value: enum.SessionStatusOpen, valid: true},
		{name: "closed", value: enum.SessionStatusClosed, valid: true},
		{name: "completed", value: enum.SessionStatusCompleted, valid: true},
		{name: "cancelled", value: enum.SessionStatusCancelled, valid: true},
		{name: "legacy published", value: "published", valid: false},
		{name: "unknown", value: "archived", valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.Valid(); got != tc.valid {
				t.Fatalf("%s.Valid() = %v, want %v", tc.value, got, tc.valid)
			}
		})
	}
}

func TestExamSessionClosedReasonValid(t *testing.T) {
	cases := []struct {
		name  string
		value enum.ExamSessionClosedReason
		valid bool
		set   bool
	}{
		{name: "empty", value: "", valid: true, set: false},
		{name: "manual", value: enum.ClosedReasonManual, valid: true, set: true},
		{name: "full", value: enum.ClosedReasonFull, valid: true, set: true},
		{name: "deadline", value: enum.ClosedReasonDeadline, valid: true, set: true},
		{name: "period_closed", value: enum.ClosedReasonPeriodClosed, valid: true, set: true},
		{name: "unknown", value: "expired", valid: false, set: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.Valid(); got != tc.valid {
				t.Fatalf("%s.Valid() = %v, want %v", tc.value, got, tc.valid)
			}
			if got := tc.value.IsSet(); got != tc.set {
				t.Fatalf("%s.IsSet() = %v, want %v", tc.value, got, tc.set)
			}
		})
	}
}

func TestExamPeriodStatusValid(t *testing.T) {
	cases := []struct {
		name  string
		value enum.ExamPeriodStatus
		valid bool
		final bool
	}{
		{name: "draft", value: enum.PeriodStatusDraft, valid: true},
		{name: "published", value: enum.PeriodStatusPublished, valid: true},
		{name: "registration_open", value: enum.PeriodStatusRegistrationOpen, valid: true},
		{name: "registration_closed", value: enum.PeriodStatusRegistrationClosed, valid: true},
		{name: "completed", value: enum.PeriodStatusCompleted, valid: true, final: true},
		{name: "cancelled", value: enum.PeriodStatusCancelled, valid: true, final: true},
		{name: "legacy rescheduled", value: "rescheduled", valid: false},
		{name: "unknown", value: "archived", valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.Valid(); got != tc.valid {
				t.Fatalf("%s.Valid() = %v, want %v", tc.value, got, tc.valid)
			}
			if got := tc.value.IsFinal(); got != tc.final {
				t.Fatalf("%s.IsFinal() = %v, want %v", tc.value, got, tc.final)
			}
			wantAccepts := tc.value == enum.PeriodStatusRegistrationOpen
			if got := tc.value.AcceptsRegistration(); got != wantAccepts {
				t.Fatalf("%s.AcceptsRegistration() = %v, want %v", tc.value, got, wantAccepts)
			}
		})
	}
}

func TestExamSeatStatusValid(t *testing.T) {
	for _, s := range []enum.ExamSeatStatus{enum.SeatAvailable, enum.SeatHeld, enum.SeatBooked} {
		if !s.Valid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if enum.ExamSeatStatus("broken").Valid() {
		t.Fatal("unknown seat status should be invalid")
	}
}

func TestToExamTypeResponseDTO(t *testing.T) {
	name := "HSK 3"
	desc := "Sơ cấp"
	got := exam.ToExamTypeResponseDTO(exam.ExamTypeEntity{
		ExamTypeID:          5,
		ExamTypeCode:        "HSK3",
		ExamTypeName:        name,
		ExamTypeDescription: &desc,
	})
	if got.ID != 5 || got.Code != "HSK3" || got.Name != name {
		t.Fatalf("unexpected mapping: %+v", got)
	}
	if got.Description == nil || *got.Description != desc {
		t.Fatalf("unexpected description: %+v", got.Description)
	}
}

// fixedNow keeps time-sensitive assertions deterministic.
var fixedNow = time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

func day(offsetDays int) *time.Time {
	d := fixedNow.AddDate(0, 0, offsetDays)
	return &d
}
