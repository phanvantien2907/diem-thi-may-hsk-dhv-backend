package request

// RegisterRequestDTO is the payload for POST /auth/register.
// Creates a pending account; activation requires OTP verification.
type RegisterRequestDTO struct {
	Phone    string `json:"phone"    validate:"required,max=20"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	Email    string `json:"email"    validate:"omitempty,email,max=150"`
}
