package request

// CreateExamSessionRequestDTO is one session in the batch-create payload.
// Status defaults to "open" (published immediately); use "draft" or "scheduled"
// to keep the session hidden until it is explicitly published.
type CreateExamSessionRequestDTO struct {
	ExamTypeID           int64   `json:"exam_type_id"          validate:"required,min=1"`
	Date                 string  `json:"date"                  validate:"required"` // YYYY-MM-DD
	Shift                string  `json:"shift"                 validate:"required,oneof=morning afternoon evening"`
	Capacity             int     `json:"capacity"              validate:"required,min=1"`
	Fee                  float64 `json:"fee"                   validate:"min=0"`
	RegistrationDeadline string  `json:"registration_deadline" validate:"required"` // RFC3339
	Status               string  `json:"status"                validate:"omitempty,oneof=open draft scheduled"`
	PublishAt            string  `json:"publish_at"            validate:"omitempty"` // RFC3339, required when status=scheduled
	CheckInAt            string  `json:"check_in_at"           validate:"omitempty"` // RFC3339
}
