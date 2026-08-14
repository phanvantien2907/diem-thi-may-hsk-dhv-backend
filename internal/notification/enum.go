package notification

// NotificationChannel enumerates supported delivery channels.
type NotificationChannel string

const (
	ChannelEmail NotificationChannel = "email"
	ChannelSMS   NotificationChannel = "sms"
	ChannelZalo  NotificationChannel = "zalo"
	ChannelPush  NotificationChannel = "push"
)

func (c NotificationChannel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelZalo, ChannelPush:
		return true
	}
	return false
}

// NotificationStatus enumerates delivery states.
type NotificationStatus string

const (
	NotifStatusPending NotificationStatus = "pending"
	NotifStatusSent    NotificationStatus = "sent"
	NotifStatusFailed  NotificationStatus = "failed"
)

func (s NotificationStatus) Valid() bool {
	switch s {
	case NotifStatusPending, NotifStatusSent, NotifStatusFailed:
		return true
	}
	return false
}
