package notification

// ToNotificationResponseDTO maps a NotificationEntity to its public DTO.
func ToNotificationResponseDTO(e NotificationEntity) NotificationResponseDTO {
	return NotificationResponseDTO{
		ID:        e.NotificationID,
		AccountID: e.NotificationAccountID,
		Channel:   string(e.NotificationChannel),
		Content:   e.NotificationContent,
		Status:    string(e.NotificationStatus),
		CreatedAt: e.NotificationCreatedAt,
	}
}
