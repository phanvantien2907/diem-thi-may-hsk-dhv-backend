package request

// LoginRequestDTO is the payload for POST /auth/login.
type LoginRequestDTO struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}
