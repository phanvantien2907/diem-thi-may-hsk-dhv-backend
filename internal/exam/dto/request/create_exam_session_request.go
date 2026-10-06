package request

// CreateExamSessionRequestDTO is one session in the batch-create payload.
type CreateExamSessionRequestDTO struct {
	ExamTypeID           int64   `json:"exam_type_id"          validate:"required,min=1"`
	Date                 string  `json:"date"                  validate:"required"` // YYYY-MM-DD
	Shift                string  `json:"shift"                 validate:"required"`
	Capacity             int     `json:"capacity"              validate:"required,min=1"`
	Fee                  float64 `json:"fee"                   validate:"min=0"`
	RegistrationDeadline string  `json:"registration_deadline" validate:"required"` // RFC3339
	PublicationStatus    string  `json:"exam_session_publication_status" validate:"omitempty,oneof=published draft scheduled"`
	PublishAt            string  `json:"exam_session_publish_at" validate:"omitempty"`  // RFC3339; required for scheduled
	CheckInAt            string  `json:"exam_session_check_in_at" validate:"omitempty"` // RFC3339
}
