package request

// RefreshTokenRequestDTO is the payload for POST /auth/refresh.
type RefreshTokenRequestDTO struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}
