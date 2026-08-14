package notification

import "github.com/hskvinh/backend/internal/notification/dto/response"

// ToNotificationResponseDTO maps a NotificationEntity to its public DTO.
func ToNotificationResponseDTO(e NotificationEntity) response.NotificationResponseDTO {
	return response.NotificationResponseDTO{
		ID:        e.NotificationID,
		AccountID: e.NotificationAccountID,
		Channel:   string(e.NotificationChannel),
		Content:   e.NotificationContent,
		Status:    string(e.NotificationStatus),
		CreatedAt: e.NotificationCreatedAt,
	}
}
