package response

import "time"

// SessionResponseDTO is the public session representation.
type SessionResponseDTO struct {
	ID         int64     `json:"id"`
	DeviceInfo *string   `json:"device_info,omitempty"`
	IPAddress  *string   `json:"ip_address,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}
