package notification

import "github.com/hskvinh/backend/internal/notification/dto/response"

// ToNotificationResponseDTO maps a NotificationEntity to its public DTO.
func ToNotificationResponseDTO(e NotificationEntity) response.NotificationResponseDTO {
	isRead := e.NotificationIsRead || string(e.NotificationStatus) == "sent"
	role := e.NotificationRole
	if role == "" {
		role = "candidate"
	}
	notifType := e.NotificationType
	if notifType == "" {
		notifType = "SYSTEM"
	}
	title := e.NotificationTitle
	if title == "" {
		title = "Thông báo hệ thống"
	}
	return response.NotificationResponseDTO{
		ID:        e.NotificationID,
		UserID:    e.NotificationAccountID,
		AccountID: e.NotificationAccountID,
		Role:      role,
		Type:      notifType,
		Title:     title,
		Content:   e.NotificationContent,
		IsRead:    isRead,
		Channel:   string(e.NotificationChannel),
		Status:    string(e.NotificationStatus),
		CreatedAt: e.NotificationCreatedAt,
	}
}
