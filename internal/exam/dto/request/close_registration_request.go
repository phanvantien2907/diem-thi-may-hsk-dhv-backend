package request

// CloseRegistrationRequestDTO carries optional reason for closing registrations early.
type CloseRegistrationRequestDTO struct {
	Reason string `json:"reason"`
}
