package response

import "time"

// ExamSessionResponseDTO is the public exam session representation.
type ExamSessionResponseDTO struct {
	ID                   int64      `json:"id"`
	ExamTypeID           int64      `json:"exam_type_id"`
	Date                 time.Time  `json:"date"`
	Shift                string     `json:"shift"`
	Capacity             int        `json:"capacity"`
	Fee                  int64      `json:"fee"`
	RegistrationDeadline time.Time  `json:"registration_deadline"`
	Status               string     `json:"status"`
	PublicationStatus    string     `json:"exam_session_publication_status"`
	PublishAt            *time.Time `json:"exam_session_publish_at,omitempty"`
	CheckInAt            *time.Time `json:"exam_session_check_in_at,omitempty"`
}
