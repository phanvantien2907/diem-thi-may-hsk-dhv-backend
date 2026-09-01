package response

import "time"

// WaitlistResponseDTO is the public waitlist entry representation.
type WaitlistResponseDTO struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"exam_session_id"`
	JoinedAt  time.Time `json:"joined_at"`
}
