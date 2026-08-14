package response

import "time"

// NotificationResponseDTO is the public notification representation.
type NotificationResponseDTO struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"account_id"`
	Channel   string    `json:"channel"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
