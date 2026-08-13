package auth

import "time"

// ── Role ──────────────────────────────────────────────────────────────────

// Role maps to the roles table.
type Role struct {
	RoleID        int64      `json:"role_id"`
	RoleCode      string     `json:"role_code"`
	RoleName      string     `json:"role_name"`
	RoleIsDeleted bool       `json:"role_is_deleted"`
	RoleDeletedAt *time.Time `json:"role_deleted_at,omitempty"`
	RoleCreatedAt time.Time  `json:"role_created_at"`
	RoleUpdatedAt time.Time  `json:"role_updated_at"`
}

// ── Account ───────────────────────────────────────────────────────────────

// AccountStatus enumerates allowed account lifecycle states.
type AccountStatus string

const (
	AccountStatusActive  AccountStatus = "active"
	AccountStatusLocked  AccountStatus = "locked"
	AccountStatusPending AccountStatus = "pending"
)

// Account maps to the accounts table.
// account_password_hash is tagged json:"-" so it is never serialised.
type Account struct {
	AccountID           int64         `json:"account_id"`
	AccountRoleID       int64         `json:"account_role_id"`
	AccountUsername     string        `json:"account_username"`
	AccountPasswordHash string        `json:"-"`
	AccountEmail        string        `json:"account_email"`
	AccountPhone        *string       `json:"account_phone,omitempty"`
	AccountStatus       AccountStatus `json:"account_status"`
	AccountLastLoginAt  *time.Time    `json:"account_last_login_at,omitempty"`
	AccountIsDeleted    bool          `json:"account_is_deleted"`
	AccountDeletedAt    *time.Time    `json:"account_deleted_at,omitempty"`
	AccountCreatedAt    time.Time     `json:"account_created_at"`
	AccountUpdatedAt    time.Time     `json:"account_updated_at"`
}

// ── DTO / request shapes ──────────────────────────────────────────────────

// RegisterInput holds the fields needed to create a new account.
type RegisterInput struct {
	Username string
	Email    string
	Password string
	Phone    string
	RoleCode string // defaults to "candidate"
}

// LoginInput holds login credentials.
type LoginInput struct {
	Username string
	Password string
}

// UpdateAccountInput holds mutable account fields.
type UpdateAccountInput struct {
	Email string
	Phone string
}

// TokenPair carries the issued JWT tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}
