package response

import "time"

// ExamRegistrationResponseDTO is the public registration representation.
type ExamRegistrationResponseDTO struct {
	ID           int64     `json:"id"`
	CandidateID  int64     `json:"candidate_id"`
	SessionID    int64     `json:"exam_session_id"`
	SeatID       int64     `json:"exam_seat_id"`
	Status       string    `json:"status"`
	RegisteredAt time.Time `json:"registered_at"`
}
