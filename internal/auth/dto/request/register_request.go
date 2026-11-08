package request

// RegisterRequestDTO is the payload for POST /auth/register.
// Supports both legacy 9-digit CMND and modern 12-digit CCCD via cccd_or_cmnd validation tag.
type RegisterRequestDTO struct {
	Cccd     string `json:"cccd"      validate:"required,cccd_or_cmnd"`
	Phone    string `json:"phone"     validate:"required,max=20"`
	FullName string `json:"full_name" validate:"required,max=150"`
	Email    string `json:"email"     validate:"required,email,max=150"`
	Password string `json:"password"  validate:"required,complex_password,max=72"`
}
