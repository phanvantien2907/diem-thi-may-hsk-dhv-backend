package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles system notifications.
type Service struct{ db *sql.DB }

// NewService creates a notification Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateNotification inserts a pending notification and returns its DTO.
func (s *Service) CreateNotification(ctx context.Context, accountID int64, channel NotificationChannel, content string) (NotificationResponseDTO, error) {
	if !channel.Valid() {
		return NotificationResponseDTO{}, fmt.Errorf("invalid notification channel: %s", channel)
	}
	var e NotificationEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO notifications
		       (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1,$2,$3,'pending')
		RETURNING notification_id, notification_account_id, notification_channel,
		          notification_content, notification_status, notification_is_deleted,
		          notification_deleted_at, notification_created_at, notification_updated_at`,
		accountID, channel, content,
	).Scan(
		&e.NotificationID, &e.NotificationAccountID, &e.NotificationChannel,
		&e.NotificationContent, &e.NotificationStatus, &e.NotificationIsDeleted,
		&e.NotificationDeletedAt, &e.NotificationCreatedAt, &e.NotificationUpdatedAt,
	)
	if err != nil {
		return NotificationResponseDTO{}, fmt.Errorf("notification: create: %w", err)
	}
	return ToNotificationResponseDTO(e), nil
}

// MarkSent transitions a notification to 'sent'.
func (s *Service) MarkSent(ctx context.Context, notificationID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET notification_status='sent', notification_updated_at=NOW()
		WHERE notification_id=$1 AND notification_is_deleted=FALSE`, notificationID)
	if err != nil {
		return fmt.Errorf("notification: mark sent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("notification not found")
	}
	return nil
}

// GetMyNotifications returns a user's non-deleted notifications as DTOs.
func (s *Service) GetMyNotifications(ctx context.Context, accountID int64) ([]NotificationResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT notification_id, notification_account_id, notification_channel,
		       notification_content, notification_status, notification_is_deleted,
		       notification_deleted_at, notification_created_at, notification_updated_at
		FROM notifications
		WHERE notification_account_id=$1 AND notification_is_deleted=FALSE
		ORDER BY notification_created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("notification: list: %w", err)
	}
	defer rows.Close()
	var list []NotificationResponseDTO
	for rows.Next() {
		var e NotificationEntity
		if err := rows.Scan(
			&e.NotificationID, &e.NotificationAccountID, &e.NotificationChannel,
			&e.NotificationContent, &e.NotificationStatus, &e.NotificationIsDeleted,
			&e.NotificationDeletedAt, &e.NotificationCreatedAt, &e.NotificationUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("notification: scan: %w", err)
		}
		list = append(list, ToNotificationResponseDTO(e))
	}
	return list, rows.Err()
}
