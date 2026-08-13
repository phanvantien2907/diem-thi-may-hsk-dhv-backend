package exam

import "time"

// ── ExamType ──────────────────────────────────────────────────────────────

// ExamType maps to the exam_types table.
type ExamType struct {
	ExamTypeID          int64      `json:"exam_type_id"`
	ExamTypeCode        string     `json:"exam_type_code"`
	ExamTypeName        string     `json:"exam_type_name"`
	ExamTypeDescription *string    `json:"exam_type_description,omitempty"`
	ExamTypeIsDeleted   bool       `json:"exam_type_is_deleted"`
	ExamTypeDeletedAt   *time.Time `json:"exam_type_deleted_at,omitempty"`
	ExamTypeCreatedAt   time.Time  `json:"exam_type_created_at"`
	ExamTypeUpdatedAt   time.Time  `json:"exam_type_updated_at"`
}

// ── ExamRoom ──────────────────────────────────────────────────────────────

// ExamRoom maps to the exam_rooms table.
type ExamRoom struct {
	ExamRoomID        int64      `json:"exam_room_id"`
	ExamRoomName      string     `json:"exam_room_name"`
	ExamRoomLocation  *string    `json:"exam_room_location,omitempty"`
	ExamRoomCapacity  int        `json:"exam_room_capacity"`
	ExamRoomIsDeleted bool       `json:"exam_room_is_deleted"`
	ExamRoomDeletedAt *time.Time `json:"exam_room_deleted_at,omitempty"`
	ExamRoomCreatedAt time.Time  `json:"exam_room_created_at"`
	ExamRoomUpdatedAt time.Time  `json:"exam_room_updated_at"`
}

// ── ExamSession ───────────────────────────────────────────────────────────

// ExamShift enumerates time-of-day slots.
type ExamShift string

const (
	ShiftMorning   ExamShift = "morning"
	ShiftAfternoon ExamShift = "afternoon"
	ShiftEvening   ExamShift = "evening"
)

// ExamSessionStatus enumerates session lifecycle states.
type ExamSessionStatus string

const (
	SessionStatusOpen      ExamSessionStatus = "open"
	SessionStatusClosed    ExamSessionStatus = "closed"
	SessionStatusCancelled ExamSessionStatus = "cancelled"
)

// ExamSession maps to the exam_sessions table.
// exam_session_fee is NUMERIC(12,0) stored as int64 (VND, no decimals).
type ExamSession struct {
	ExamSessionID                   int64             `json:"exam_session_id"`
	ExamSessionExamTypeID           int64             `json:"exam_session_exam_type_id"`
	ExamSessionExamRoomID           int64             `json:"exam_session_exam_room_id"`
	ExamSessionDate                 time.Time         `json:"exam_session_date"`
	ExamSessionShift                ExamShift         `json:"exam_session_shift"`
	ExamSessionCapacity             int               `json:"exam_session_capacity"`
	ExamSessionFee                  int64             `json:"exam_session_fee"`
	ExamSessionRegistrationDeadline time.Time         `json:"exam_session_registration_deadline"`
	ExamSessionStatus               ExamSessionStatus `json:"exam_session_status"`
	ExamSessionIsDeleted            bool              `json:"exam_session_is_deleted"`
	ExamSessionDeletedAt            *time.Time        `json:"exam_session_deleted_at,omitempty"`
	ExamSessionCreatedAt            time.Time         `json:"exam_session_created_at"`
	ExamSessionUpdatedAt            time.Time         `json:"exam_session_updated_at"`
}

// ── ExamSeat ──────────────────────────────────────────────────────────────

// ExamSeatStatus enumerates seat availability states.
type ExamSeatStatus string

const (
	SeatAvailable ExamSeatStatus = "available"
	SeatHeld      ExamSeatStatus = "held"
	SeatBooked    ExamSeatStatus = "booked"
)

// ExamSeat maps to the exam_seats table.
type ExamSeat struct {
	ExamSeatID            int64          `json:"exam_seat_id"`
	ExamSeatExamSessionID int64          `json:"exam_seat_exam_session_id"`
	ExamSeatNumber        int16          `json:"exam_seat_number"`
	ExamSeatStatus        ExamSeatStatus `json:"exam_seat_status"`
	ExamSeatHeldUntil     *time.Time     `json:"exam_seat_held_until,omitempty"`
	ExamSeatIsDeleted     bool           `json:"exam_seat_is_deleted"`
	ExamSeatDeletedAt     *time.Time     `json:"exam_seat_deleted_at,omitempty"`
	ExamSeatCreatedAt     time.Time      `json:"exam_seat_created_at"`
	ExamSeatUpdatedAt     time.Time      `json:"exam_seat_updated_at"`
}
