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

// CreateNotification inserts a pending notification.
func (s *Service) CreateNotification(ctx context.Context, accountID int64, channel NotificationChannel, content string) (*Notification, error) {
	var n Notification
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO notifications
		       (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1,$2,$3,'pending')
		RETURNING notification_id, notification_account_id, notification_channel,
		          notification_content, notification_status, notification_is_deleted,
		          notification_created_at, notification_updated_at`,
		accountID, channel, content,
	).Scan(
		&n.NotificationID, &n.NotificationAccountID, &n.NotificationChannel,
		&n.NotificationContent, &n.NotificationStatus, &n.NotificationIsDeleted,
		&n.NotificationCreatedAt, &n.NotificationUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("notification: create: %w", err)
	}
	return &n, nil
}

// MarkSent transitions a notification to 'sent'.
func (s *Service) MarkSent(ctx context.Context, notificationID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET notification_status = 'sent', notification_updated_at = NOW()
		WHERE notification_id = $1 AND notification_is_deleted = FALSE`,
		notificationID,
	)
	if err != nil {
		return fmt.Errorf("notification: mark sent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("notification not found")
	}
	return nil
}

// GetMyNotifications returns non-deleted notifications for an account.
func (s *Service) GetMyNotifications(ctx context.Context, accountID int64) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT notification_id, notification_account_id, notification_channel,
		       notification_content, notification_status, notification_is_deleted,
		       notification_created_at, notification_updated_at
		FROM notifications
		WHERE notification_account_id = $1 AND notification_is_deleted = FALSE
		ORDER BY notification_created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("notification: list: %w", err)
	}
	defer rows.Close()
	var list []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(
			&n.NotificationID, &n.NotificationAccountID, &n.NotificationChannel,
			&n.NotificationContent, &n.NotificationStatus, &n.NotificationIsDeleted,
			&n.NotificationCreatedAt, &n.NotificationUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("notification: scan: %w", err)
		}
		list = append(list, n)
	}
	return list, rows.Err()
}
