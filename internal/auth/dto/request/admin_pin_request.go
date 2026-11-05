package request

// SetupAdminPinRequestDTO is used by an authenticated admin to set or reset their 6-digit PII security PIN.
type SetupAdminPinRequestDTO struct {
	Pin             string `json:"pin" validate:"required,len=6,numeric"`
	CurrentPassword string `json:"current_password,omitempty"`
}

// VerifyAdminPinRequestDTO is used by an authenticated admin to verify their 6-digit PII security PIN
// in exchange for a 5-minute pii_access_token.
type VerifyAdminPinRequestDTO struct {
	Pin string `json:"pin" validate:"required,len=6,numeric"`
}

