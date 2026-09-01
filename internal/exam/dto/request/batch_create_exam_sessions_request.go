package request

// BatchCreateExamSessionsRequestDTO is the payload for POST /admin/exam-sessions/batch-create.
type BatchCreateExamSessionsRequestDTO struct {
	Sessions []CreateExamSessionRequestDTO `json:"sessions" validate:"required,min=1,dive"`
}
