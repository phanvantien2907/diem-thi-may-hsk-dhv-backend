package request

// LoginRequestDTO is the payload for POST /auth/login.
type LoginRequestDTO struct {
	Identifier string `json:"identifier" validate:"required"` // Can be Email or CCCD
	Password   string `json:"password"   validate:"required"`
}
