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

// ExamSessionStatus enumerates session lifecycle states.
type ExamSessionStatus string

const (
	SessionStatusOpen      ExamSessionStatus = "open"
	SessionStatusClosed    ExamSessionStatus = "closed"
	SessionStatusCancelled ExamSessionStatus = "cancelled"
)

// Valid reports whether s is a recognised ExamSessionStatus.
func (s ExamSessionStatus) Valid() bool {
	switch s {
	case SessionStatusOpen, SessionStatusClosed, SessionStatusCancelled:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamSessionStatus) String() string { return string(s) }

// ExamSessionPublicationStatus controls when a session is visible to candidates.
type ExamSessionPublicationStatus string

const (
	PublicationStatusPublished ExamSessionPublicationStatus = "published"
	PublicationStatusDraft     ExamSessionPublicationStatus = "draft"
	PublicationStatusScheduled ExamSessionPublicationStatus = "scheduled"
)

// Valid reports whether s is a recognised publication status.
func (s ExamSessionPublicationStatus) Valid() bool {
	switch s {
	case PublicationStatusPublished, PublicationStatusDraft, PublicationStatusScheduled:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamSessionPublicationStatus) String() string { return string(s) }

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

// ExamPeriodStatus enumerates period lifecycle states.
type ExamPeriodStatus string

const (
	PeriodStatusDraft              ExamPeriodStatus = "draft"
	PeriodStatusPublished          ExamPeriodStatus = "published"
	PeriodStatusRegistrationOpen   ExamPeriodStatus = "registration_open"
	PeriodStatusRegistrationClosed ExamPeriodStatus = "registration_closed"
	PeriodStatusRescheduled        ExamPeriodStatus = "rescheduled"
	PeriodStatusCancelled          ExamPeriodStatus = "cancelled"
	PeriodStatusCompleted          ExamPeriodStatus = "completed"
)

// Valid reports whether s is a recognised ExamPeriodStatus.
func (s ExamPeriodStatus) Valid() bool {
	switch s {
	case PeriodStatusDraft, PeriodStatusPublished, PeriodStatusRegistrationOpen,
		PeriodStatusRegistrationClosed, PeriodStatusRescheduled,
		PeriodStatusCancelled, PeriodStatusCompleted:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamPeriodStatus) String() string { return string(s) }
