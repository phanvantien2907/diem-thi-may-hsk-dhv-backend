package response

import "time"

// NotificationResponseDTO is the public notification representation sent via REST and WebSocket (NOTIFICATION_PAYLOAD).
type NotificationResponseDTO struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	AccountID int64     `json:"account_id"`
	Role      string    `json:"role"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	IsRead    bool      `json:"is_read"`
	Channel   string    `json:"channel"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
