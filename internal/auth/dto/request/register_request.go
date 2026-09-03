package request

// RegisterRequestDTO is the payload for POST /auth/register.
// Creates a pending account; activation requires OTP verification.
type RegisterRequestDTO struct {
	Cccd     string `json:"cccd"      validate:"required,len=12,numeric"`
	Phone    string `json:"phone"     validate:"required,max=20"`
	FullName string `json:"full_name" validate:"required,max=150"`
	Email    string `json:"email"     validate:"required,email,max=150"`
	Password string `json:"password"  validate:"required,complex_password,max=72"`
}
