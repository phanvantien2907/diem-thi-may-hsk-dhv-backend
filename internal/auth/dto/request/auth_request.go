package request

// CreateAccountRequestDTO is the payload for POST /auth/register.
type CreateAccountRequestDTO struct {
	Username string `json:"username"  validate:"required,min=3,max=50"`
	Email    string `json:"email"     validate:"required,email,max=150"`
	Password string `json:"password"  validate:"required,min=8,max=72"`
	Phone    string `json:"phone"     validate:"omitempty,max=20"`
	RoleCode string `json:"role_code" validate:"required"`
}

// LoginRequestDTO is the payload for POST /auth/login.
type LoginRequestDTO struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// UpdateAccountRequestDTO is the payload for PUT /me.
type UpdateAccountRequestDTO struct {
	Email string `json:"email" validate:"omitempty,email,max=150"`
	Phone string `json:"phone" validate:"omitempty,max=20"`
}
