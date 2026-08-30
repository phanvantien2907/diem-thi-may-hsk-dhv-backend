package entities

import "time"

// SessionEntity mirrors the auth_sessions table 1:1.
type SessionEntity struct {
	SessionID               int64
	SessionAccountID        int64
	SessionRefreshTokenHash string
	SessionDeviceInfo       *string
	SessionIPAddress        *string
	SessionExpiresAt        time.Time
	SessionRevokedAt        *time.Time
	SessionCreatedAt        time.Time
}
