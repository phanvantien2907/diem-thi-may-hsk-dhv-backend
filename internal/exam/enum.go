package exam

// ExamShift enumerates time-of-day slots.
type ExamShift string

const (
	ShiftMorning   ExamShift = "morning"
	ShiftAfternoon ExamShift = "afternoon"
	ShiftEvening   ExamShift = "evening"
)

func (s ExamShift) Valid() bool {
	switch s {
	case ShiftMorning, ShiftAfternoon, ShiftEvening:
		return true
	}
	return false
}

// ExamSessionStatus enumerates session lifecycle states.
type ExamSessionStatus string

const (
	SessionStatusOpen      ExamSessionStatus = "open"
	SessionStatusClosed    ExamSessionStatus = "closed"
	SessionStatusCancelled ExamSessionStatus = "cancelled"
)

func (s ExamSessionStatus) Valid() bool {
	switch s {
	case SessionStatusOpen, SessionStatusClosed, SessionStatusCancelled:
		return true
	}
	return false
}

// ExamSeatStatus enumerates seat availability states.
type ExamSeatStatus string

const (
	SeatAvailable ExamSeatStatus = "available"
	SeatHeld      ExamSeatStatus = "held"
	SeatBooked    ExamSeatStatus = "booked"
)

func (s ExamSeatStatus) Valid() bool {
	switch s {
	case SeatAvailable, SeatHeld, SeatBooked:
		return true
	}
	return false
}
