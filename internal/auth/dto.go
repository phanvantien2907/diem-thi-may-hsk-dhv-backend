package auth

import "time"

// ── Request DTOs ──────────────────────────────────────────────────────────────

// CreateAccountRequestDTO is the payload for POST /auth/register.
type CreateAccountRequestDTO struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Email    string `json:"email"    validate:"required,email,max=150"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	Phone    string `json:"phone"    validate:"omitempty,max=20"`
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

// ── Response DTOs ─────────────────────────────────────────────────────────────

// AccountResponseDTO is the public account representation.
// account_password_hash is intentionally omitted.
type AccountResponseDTO struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Phone     *string   `json:"phone,omitempty"`
	Status    string    `json:"status"`
	RoleID    int64     `json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TokenPairResponseDTO carries the issued JWT pair.
type TokenPairResponseDTO struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}
