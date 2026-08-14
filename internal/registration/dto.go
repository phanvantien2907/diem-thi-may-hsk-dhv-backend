package registration

import "time"

// ── Request DTOs ──────────────────────────────────────────────────────────────

// ReserveSeatRequestDTO is the payload for POST /registrations.
type ReserveSeatRequestDTO struct {
	ExamSessionID int64 `json:"exam_session_id" validate:"required,min=1"`
	CandidateID   int64 `json:"candidate_id"    validate:"required,min=1"`
}

// ── Response DTOs ─────────────────────────────────────────────────────────────

// ExamRegistrationResponseDTO is the public registration representation.
type ExamRegistrationResponseDTO struct {
	ID           int64     `json:"id"`
	CandidateID  int64     `json:"candidate_id"`
	SessionID    int64     `json:"exam_session_id"`
	SeatID       int64     `json:"exam_seat_id"`
	Status       string    `json:"status"`
	RegisteredAt time.Time `json:"registered_at"`
	CreatedAt    time.Time `json:"created_at"`
}
