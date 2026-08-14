package response

import "time"

// ExamTypeResponseDTO is the public exam type representation.
type ExamTypeResponseDTO struct {
	ID          int64   `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ExamRoomResponseDTO is the public exam room representation.
type ExamRoomResponseDTO struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Location *string `json:"location,omitempty"`
	Capacity int     `json:"capacity"`
}

// ExamSessionResponseDTO is the public exam session representation.
type ExamSessionResponseDTO struct {
	ID                   int64     `json:"id"`
	ExamTypeID           int64     `json:"exam_type_id"`
	ExamRoomID           int64     `json:"exam_room_id"`
	Date                 time.Time `json:"date"`
	Shift                string    `json:"shift"`
	Capacity             int       `json:"capacity"`
	Fee                  int64     `json:"fee"`
	RegistrationDeadline time.Time `json:"registration_deadline"`
	Status               string    `json:"status"`
}

// ExamSeatResponseDTO is the public seat representation.
type ExamSeatResponseDTO struct {
	ID        int64      `json:"id"`
	SessionID int64      `json:"session_id"`
	Number    int16      `json:"number"`
	Status    string     `json:"status"`
	HeldUntil *time.Time `json:"held_until,omitempty"`
}

// GenerateSeatsResponseDTO is returned after admin seat generation.
type GenerateSeatsResponseDTO struct {
	SessionID    int64 `json:"session_id"`
	CreatedSeats int   `json:"created_seats"`
}
