package request

// ReserveSeatRequestDTO is the payload for POST /registrations.
type ReserveSeatRequestDTO struct {
	ExamSessionID int64 `json:"exam_session_id" validate:"required,min=1"`
	CandidateID   int64 `json:"candidate_id"    validate:"required,min=1"`
}
