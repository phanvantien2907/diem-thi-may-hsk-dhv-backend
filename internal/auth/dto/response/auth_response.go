package response

import "time"

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
