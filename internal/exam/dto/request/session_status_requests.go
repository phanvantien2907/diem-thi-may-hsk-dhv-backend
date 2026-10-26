package request

// PublishSessionRequestDTO publishes a session immediately, or schedules the
// publication when PublishAt is a future RFC3339 timestamp.
type PublishSessionRequestDTO struct {
	PublishAt string `json:"publish_at" validate:"omitempty"` // RFC3339
}

// ReopenSessionRegistrationRequestDTO reopens a closed session. A new deadline is
// mandatory when the session was closed because its deadline elapsed.
type ReopenSessionRegistrationRequestDTO struct {
	RegistrationDeadline string `json:"registration_deadline" validate:"omitempty"` // RFC3339
}

// UpdateCapacityRequestDTO changes a session's seat capacity.
type UpdateCapacityRequestDTO struct {
	Capacity int `json:"capacity" validate:"required,min=1"`
}
