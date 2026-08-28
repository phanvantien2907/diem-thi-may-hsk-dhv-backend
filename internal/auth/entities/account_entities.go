package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/auth/enum"
)

// AccountEntity mirrors the accounts table 1:1.
type AccountEntity struct {
	AccountID           int64
	AccountRoleID       int64
	AccountUsername     string
	AccountPasswordHash string
	AccountEmail        string
	AccountPhone        *string
	AccountStatus       enum.AccountStatus
	AccountLastLoginAt  *time.Time
	AccountIsDeleted    bool
	AccountDeletedAt    *time.Time
	AccountCreatedAt    time.Time
	AccountUpdatedAt    time.Time
}
