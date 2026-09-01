package request

// CreateExamSessionRequestDTO is one session in the batch-create payload.
type CreateExamSessionRequestDTO struct {
	ExamTypeID           int64   `json:"exam_type_id"          validate:"required,min=1"`
	ExamRoomID           int64   `json:"exam_room_id"          validate:"required,min=1"`
	Date                 string  `json:"date"                  validate:"required"` // YYYY-MM-DD
	Shift                string  `json:"shift"                 validate:"required"`
	Capacity             int     `json:"capacity"              validate:"required,min=1"`
	Fee                  float64 `json:"fee"                   validate:"min=0"`
	RegistrationDeadline string  `json:"registration_deadline" validate:"required"` // RFC3339
}
