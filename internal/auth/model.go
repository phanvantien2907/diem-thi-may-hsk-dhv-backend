package auth

import "time"

// AccountEntity mirrors the accounts table 1:1.
// Enums are typed via AccountStatus (enum.go).
// DTOs for request/response are in dto.go.
type AccountEntity struct {
	AccountID            int64
	AccountRoleID        int64
	AccountUsername      string
	AccountPasswordHash  string
	AccountEmail         string
	AccountPhone         *string
	AccountStatus        AccountStatus
	AccountLastLoginAt   *time.Time
	AccountIsDeleted     bool
	AccountDeletedAt     *time.Time
	AccountCreatedAt     time.Time
	AccountUpdatedAt     time.Time
}

// RoleEntity mirrors the roles table 1:1.
type RoleEntity struct {
	RoleID        int64
	RoleCode      string
	RoleName      string
	RoleIsDeleted bool
	RoleCreatedAt time.Time
	RoleUpdatedAt time.Time
}

// ── Internal service inputs (not exposed as DTOs) ─────────────────────────────

// RegisterInput carries validated data from CreateAccountRequestDTO into the service.
type RegisterInput struct {
	Username string
	Email    string
	Password string
	Phone    string
	RoleCode string
}

// LoginInput carries validated credentials from LoginRequestDTO into the service.
type LoginInput struct {
	Username string
	Password string
}

// UpdateAccountInput carries validated updates from UpdateAccountRequestDTO into the service.
type UpdateAccountInput struct {
	Email string
	Phone string
}
