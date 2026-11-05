package response

import "time"

// AccountResponseDTO is the public account representation.
// account_password_hash and account_pii_pin_hash are intentionally omitted.
type AccountResponseDTO struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	FullName  string    `json:"full_name"`
	Email     string    `json:"email"`
	Phone     *string   `json:"phone,omitempty"`
	Status    string    `json:"status"`
	RoleID    int64     `json:"role_id"`
	HasPiiPin bool      `json:"has_pii_pin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
