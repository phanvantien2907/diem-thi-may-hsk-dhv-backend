package response

import "time"

// AdminPinStatusResponseDTO reports whether the admin has configured a 6-digit PII PIN and any active lockout state.
type AdminPinStatusResponseDTO struct {
	HasPin               bool       `json:"has_pin"`
	IsLocked             bool       `json:"is_locked"`
	LockedUntil          *time.Time `json:"locked_until,omitempty"`
	RemainingLockSeconds int64      `json:"remaining_lock_seconds"`
	FailedCount          int        `json:"failed_count"`
	MaxAttempts          int        `json:"max_attempts"`
	RemainingAttempts    int        `json:"remaining_attempts"`
}

// VerifyAdminPinResponseDTO returns the short-lived (5-minute) JWT scoped strictly to `pii_access`.
type VerifyAdminPinResponseDTO struct {
	PiiAccessToken string `json:"pii_access_token"`
	Scope          string `json:"scope"`
	ExpiresIn      int64  `json:"expires_in"`
	ExpiresAt      string `json:"expires_at"`
}

