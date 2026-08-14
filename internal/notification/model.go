package notification

import "time"

// NotificationEntity mirrors the notifications table 1:1.
type NotificationEntity struct {
	NotificationID         int64
	NotificationAccountID  int64
	NotificationChannel    NotificationChannel
	NotificationContent    string
	NotificationStatus     NotificationStatus
	NotificationIsDeleted  bool
	NotificationDeletedAt  *time.Time
	NotificationCreatedAt  time.Time
	NotificationUpdatedAt  time.Time
}
