package response

import "time"

// ExamSeatResponseDTO is the public seat representation.
type ExamSeatResponseDTO struct {
	ID        int64      `json:"id"`
	SessionID int64      `json:"session_id"`
	Number    int16      `json:"number"`
	Status    string     `json:"status"`
	HeldUntil *time.Time `json:"held_until,omitempty"`
}
