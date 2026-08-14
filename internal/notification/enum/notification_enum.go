package enum

// NotificationChannel enumerates supported delivery channels.
type NotificationChannel string

const (
	ChannelEmail NotificationChannel = "email"
	ChannelSMS   NotificationChannel = "sms"
	ChannelZalo  NotificationChannel = "zalo"
	ChannelPush  NotificationChannel = "push"
)

// Valid reports whether c is a recognised NotificationChannel.
func (c NotificationChannel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelZalo, ChannelPush:
		return true
	}
	return false
}

// String returns the string representation.
func (c NotificationChannel) String() string { return string(c) }

// NotificationStatus enumerates delivery states.
type NotificationStatus string

const (
	NotifStatusPending NotificationStatus = "pending"
	NotifStatusSent    NotificationStatus = "sent"
	NotifStatusFailed  NotificationStatus = "failed"
)

// Valid reports whether s is a recognised NotificationStatus.
func (s NotificationStatus) Valid() bool {
	switch s {
	case NotifStatusPending, NotifStatusSent, NotifStatusFailed:
		return true
	}
	return false
}

// String returns the string representation.
func (s NotificationStatus) String() string { return string(s) }
