package response

import (
	"time"
)

// ExamSessionResponseDTO is the public exam session representation.
// `status` is always the effective, time-derived status; `stored_status` keeps the
// persisted column for display/debugging only.
type ExamSessionResponseDTO struct {
	ID                   int64      `json:"id"`
	PeriodID             *int64     `json:"period_id,omitempty"`
	ExamTypeID           int64      `json:"exam_type_id"`
	Name                 *string    `json:"name,omitempty"`
	Description          *string    `json:"description,omitempty"`
	Date                 *time.Time `json:"date"`
	Shift                string     `json:"shift"`
	Capacity             *int       `json:"capacity"`
	RegisteredCount      int        `json:"registered_count"`
	AvailableSeats       int        `json:"available_seats"`
	AvailableSlots       int        `json:"available_slots"`
	Fee                  int64      `json:"fee"`
	RegistrationDeadline *time.Time `json:"registration_deadline"`
	Status               string     `json:"status"`
	StoredStatus         string     `json:"stored_status"`
	ClosedReason         *string    `json:"closed_reason"`
	PublishAt            *time.Time `json:"publish_at,omitempty"`
	CheckInAt            *time.Time `json:"check_in_at,omitempty"`
	AvailableActions     []string   `json:"available_actions"`
}

// UpdateSessionStatusResponseDTO is returned after publishing, scheduling,
// unpublishing, closing, reopening, completing, or cancelling a session.
type UpdateSessionStatusResponseDTO struct {
	Message   string                 `json:"message"`
	PublishAt *time.Time             `json:"publish_at,omitempty"`
	Session   ExamSessionResponseDTO `json:"session"`
}
