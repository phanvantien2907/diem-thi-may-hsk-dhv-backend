package request

// CreateResultRequestDTO is the admin payload for POST /admin/results.
type CreateResultRequestDTO struct {
	ExamRegistrationID int64   `json:"exam_registration_id" validate:"required,min=1"`
	ListeningScore     float64 `json:"listening_score"      validate:"min=0,max=100"`
	ReadingScore       float64 `json:"reading_score"        validate:"min=0,max=100"`
	WritingScore       float64 `json:"writing_score"        validate:"min=0,max=100"`
	TotalScore         float64 `json:"total_score"          validate:"min=0"`
	Status             string  `json:"status"               validate:"required,oneof=pass fail"`
}

// CreateRecheckRequestDTO is the payload for POST /rechecks.
type CreateRecheckRequestDTO struct {
	ExamResultID int64  `json:"exam_result_id" validate:"required,min=1"`
	Reason       string `json:"reason"         validate:"required,min=10"`
}
