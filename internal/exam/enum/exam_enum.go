package enum

// ExamShift enumerates time-of-day exam slots.
type ExamShift string

const (
	ShiftMorning   ExamShift = "morning"
	ShiftAfternoon ExamShift = "afternoon"
	ShiftEvening   ExamShift = "evening"
)

// Valid reports whether s is a recognised ExamShift.
func (s ExamShift) Valid() bool {
	switch s {
	case ShiftMorning, ShiftAfternoon, ShiftEvening:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamShift) String() string { return string(s) }

// ExamSessionStatus enumerates session lifecycle states:
// draft -> scheduled -> open -> closed -> completed, plus the cancelled branch.
type ExamSessionStatus string

const (
	SessionStatusDraft     ExamSessionStatus = "draft"
	SessionStatusScheduled ExamSessionStatus = "scheduled"
	SessionStatusOpen      ExamSessionStatus = "open"
	SessionStatusClosed    ExamSessionStatus = "closed"
	SessionStatusCompleted ExamSessionStatus = "completed"
	SessionStatusCancelled ExamSessionStatus = "cancelled"
)

// Valid reports whether s is a recognised ExamSessionStatus.
func (s ExamSessionStatus) Valid() bool {
	switch s {
	case SessionStatusDraft, SessionStatusScheduled, SessionStatusOpen,
		SessionStatusClosed, SessionStatusCompleted, SessionStatusCancelled:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamSessionStatus) String() string { return string(s) }

// ExamSessionClosedReason explains why a session reached the `closed` state.
// "Đã đóng" và "Đã hết hạn" chỉ là hai cách hiển thị của cùng một trạng thái closed.
type ExamSessionClosedReason string

const (
	ClosedReasonManual       ExamSessionClosedReason = "manual"
	ClosedReasonFull         ExamSessionClosedReason = "full"
	ClosedReasonDeadline     ExamSessionClosedReason = "deadline"
	ClosedReasonPeriodClosed ExamSessionClosedReason = "period_closed"
)

// Valid reports whether r is a recognised ExamSessionClosedReason.
// The empty value is valid and means "not closed".
func (r ExamSessionClosedReason) Valid() bool {
	switch r {
	case "", ClosedReasonManual, ClosedReasonFull, ClosedReasonDeadline, ClosedReasonPeriodClosed:
		return true
	}
	return false
}

// IsSet reports whether a concrete reason is recorded.
func (r ExamSessionClosedReason) IsSet() bool { return r != "" }

// String returns the string representation.
func (r ExamSessionClosedReason) String() string { return string(r) }

// ExamSeatStatus enumerates seat availability states.
type ExamSeatStatus string

const (
	SeatAvailable ExamSeatStatus = "available"
	SeatHeld      ExamSeatStatus = "held"
	SeatBooked    ExamSeatStatus = "booked"
)

// Valid reports whether s is a recognised ExamSeatStatus.
func (s ExamSeatStatus) Valid() bool {
	switch s {
	case SeatAvailable, SeatHeld, SeatBooked:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamSeatStatus) String() string { return string(s) }

// ExamPeriodStatus enumerates period lifecycle states:
// draft -> published -> registration_open -> registration_closed -> completed,
// plus the cancelled branch. Rescheduling never changes the status.
type ExamPeriodStatus string

const (
	PeriodStatusDraft              ExamPeriodStatus = "draft"
	PeriodStatusPublished          ExamPeriodStatus = "published"
	PeriodStatusRegistrationOpen   ExamPeriodStatus = "registration_open"
	PeriodStatusRegistrationClosed ExamPeriodStatus = "registration_closed"
	PeriodStatusCancelled          ExamPeriodStatus = "cancelled"
	PeriodStatusCompleted          ExamPeriodStatus = "completed"
)

// Valid reports whether s is a recognised ExamPeriodStatus.
func (s ExamPeriodStatus) Valid() bool {
	switch s {
	case PeriodStatusDraft, PeriodStatusPublished, PeriodStatusRegistrationOpen,
		PeriodStatusRegistrationClosed, PeriodStatusCancelled, PeriodStatusCompleted:
		return true
	}
	return false
}

// IsFinal reports whether the status terminates the period lifecycle.
func (s ExamPeriodStatus) IsFinal() bool {
	return s == PeriodStatusCancelled || s == PeriodStatusCompleted
}

// AcceptsRegistration reports whether candidates may register while the period is in this state.
func (s ExamPeriodStatus) AcceptsRegistration() bool { return s == PeriodStatusRegistrationOpen }

// String returns the string representation.
func (s ExamPeriodStatus) String() string { return string(s) }
