package notification

import (
	"time"

	"github.com/hskvinh/backend/internal/notification/enum"
)

// NotificationEntity mirrors the notifications table 1:1.
type NotificationEntity struct {
	NotificationID        int64
	NotificationAccountID int64
	NotificationRole      string
	NotificationType      string
	NotificationTitle     string
	NotificationChannel   enum.NotificationChannel
	NotificationContent   string
	NotificationIsRead    bool
	NotificationStatus    enum.NotificationStatus
	NotificationIsDeleted bool
	NotificationDeletedAt *time.Time
	NotificationCreatedAt time.Time
	NotificationUpdatedAt time.Time
}
