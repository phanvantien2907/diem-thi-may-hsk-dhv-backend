package response

import "time"

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
