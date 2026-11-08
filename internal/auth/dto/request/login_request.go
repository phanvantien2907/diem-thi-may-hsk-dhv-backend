package request

// LoginRequestDTO is the payload for POST /auth/login.
// Identifier can be an Email address, a 9-digit CMND, or a 12-digit CCCD.
type LoginRequestDTO struct {
	Identifier string `json:"identifier" validate:"required"`
	Password   string `json:"password"   validate:"required"`
}
