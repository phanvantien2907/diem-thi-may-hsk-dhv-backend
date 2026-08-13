package notification

import "time"

// NotificationChannel enumerates supported delivery channels.
type NotificationChannel string

const (
	ChannelEmail NotificationChannel = "email"
	ChannelSMS   NotificationChannel = "sms"
	ChannelZalo  NotificationChannel = "zalo"
	ChannelPush  NotificationChannel = "push"
)

// NotificationStatus enumerates delivery states.
type NotificationStatus string

const (
	NotifStatusPending NotificationStatus = "pending"
	NotifStatusSent    NotificationStatus = "sent"
	NotifStatusFailed  NotificationStatus = "failed"
)

// Notification maps to the notifications partitioned table.
type Notification struct {
	NotificationID        int64               `json:"notification_id"`
	NotificationAccountID int64               `json:"notification_account_id"`
	NotificationChannel   NotificationChannel `json:"notification_channel"`
	NotificationContent   string              `json:"notification_content"`
	NotificationStatus    NotificationStatus  `json:"notification_status"`
	NotificationIsDeleted bool                `json:"notification_is_deleted"`
	NotificationDeletedAt *time.Time          `json:"notification_deleted_at,omitempty"`
	NotificationCreatedAt time.Time           `json:"notification_created_at"`
	NotificationUpdatedAt time.Time           `json:"notification_updated_at"`
}
