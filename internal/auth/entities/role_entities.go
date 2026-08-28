package entities

import (
	"time"
)

// RoleEntity mirrors the roles table 1:1.
type RoleEntity struct {
	RoleID        int64
	RoleCode      string
	RoleName      string
	RoleIsDeleted bool
	RoleCreatedAt time.Time
	RoleUpdatedAt time.Time
}
