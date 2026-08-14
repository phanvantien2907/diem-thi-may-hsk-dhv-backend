package result

import "time"

// ── Request DTOs ──────────────────────────────────────────────────────────────

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

// ── Response DTOs ─────────────────────────────────────────────────────────────

// ExamResultResponseDTO is the public result representation.
type ExamResultResponseDTO struct {
	ID             int64      `json:"id"`
	RegistrationID int64      `json:"exam_registration_id"`
	Listening      *float64   `json:"listening_score,omitempty"`
	Reading        *float64   `json:"reading_score,omitempty"`
	Writing        *float64   `json:"writing_score,omitempty"`
	Total          *float64   `json:"total_score,omitempty"`
	Status         *string    `json:"status,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// RecheckResponseDTO is the public recheck request representation.
type RecheckResponseDTO struct {
	ID            int64      `json:"id"`
	ExamResultID  int64      `json:"exam_result_id"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	NewTotalScore *float64   `json:"new_total_score,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}
