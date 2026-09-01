package request

// ReserveSeatRequestDTO is the payload for POST /me/registrations.
// CandidateID is resolved from JWT — not from request body.
type ReserveSeatRequestDTO struct {
	ExamSessionID int64 `json:"exam_session_id" validate:"required,min=1"`
}
