package request

// ReopenPeriodRegistrationRequestDTO reopens a closed period's registration.
// A new registration deadline in the future is mandatory.
type ReopenPeriodRegistrationRequestDTO struct {
	RegistrationDeadline string `json:"registration_deadline" validate:"required"` // RFC3339
}

// CancelPeriodRequestDTO cancels a period. Every affected registration must choose
// between a refund and a transfer to another session, so the chosen policy is required.
type CancelPeriodRequestDTO struct {
	Reason        string `json:"reason"        validate:"required,max=500"`
	Resolution    string `json:"resolution"    validate:"required,oneof=refund transfer"`
	TargetSession int64  `json:"target_session_id" validate:"omitempty,min=1"`
}
