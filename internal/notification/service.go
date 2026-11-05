package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/hskvinh/backend/internal/notification/dto/response"
	"github.com/hskvinh/backend/internal/notification/enum"
	"github.com/hskvinh/backend/internal/ws"
)

const notificationColumns = `
	notification_id,
	notification_account_id,
	COALESCE(notification_role, 'candidate'),
	COALESCE(notification_type, 'SYSTEM'),
	COALESCE(notification_title, 'Thông báo hệ thống'),
	notification_channel,
	notification_content,
	COALESCE(notification_is_read, FALSE),
	notification_status,
	notification_is_deleted,
	notification_deleted_at,
	notification_created_at,
	notification_updated_at`

func scanNotification(sc interface{ Scan(dest ...any) error }, e *NotificationEntity) error {
	return sc.Scan(
		&e.NotificationID,
		&e.NotificationAccountID,
		&e.NotificationRole,
		&e.NotificationType,
		&e.NotificationTitle,
		&e.NotificationChannel,
		&e.NotificationContent,
		&e.NotificationIsRead,
		&e.NotificationStatus,
		&e.NotificationIsDeleted,
		&e.NotificationDeletedAt,
		&e.NotificationCreatedAt,
		&e.NotificationUpdatedAt,
	)
}

// Service handles system notifications, the internal Go Channel event bus, and real-time WebSocket pushing.
type Service struct {
	db      *sql.DB
	hub     *ws.Hub
	eventCh chan DomainEvent
}

// NewService creates a notification Service with an internal buffered Go Channel event bus.
func NewService(db *sql.DB) *Service {
	return &Service{
		db:      db,
		eventCh: make(chan DomainEvent, 512),
	}
}

// SetHub attaches the WebSocket Hub for targeted real-time notification delivery.
func (s *Service) SetHub(hub *ws.Hub) {
	s.hub = hub
}

// Publish dispatches a DomainEvent onto the internal channel without blocking the caller's transaction.
func (s *Service) Publish(evt DomainEvent) {
	if s == nil || s.eventCh == nil {
		return
	}
	select {
	case s.eventCh <- evt:
	default:
		log.Printf("[notification] event bus buffer full, dropping event type=%s user_id=%d session_id=%d", evt.Type, evt.UserID, evt.SessionID)
	}
}

// StartEventListener runs a background loop consuming DomainEvents from the internal Go channel,
// persisting each notification to PostgreSQL, and pushing NOTIFICATION_PAYLOAD to the WebSocket Hub.
func (s *Service) StartEventListener(ctx context.Context) {
	log.Println("[notification] event listener started")
	for {
		select {
		case <-ctx.Done():
			log.Println("[notification] event listener stopped")
			return
		case evt := <-s.eventCh:
			s.ProcessEvent(ctx, evt)
		}
	}
}

// ProcessEvent persists a DomainEvent into the notifications table and pushes it to the WebSocket Hub.
// Exported so unit tests can also invoke it synchronously when needed.
func (s *Service) ProcessEvent(ctx context.Context, evt DomainEvent) {
	role := strings.TrimSpace(evt.Role)
	if role == "" {
		role = "candidate"
	}
	notifType := strings.TrimSpace(evt.Type.String())
	if notifType == "" {
		notifType = string(TypeSystemAlert)
	}
	title := strings.TrimSpace(evt.Title)
	if title == "" {
		title = "Thông báo hệ thống"
	}
	content := strings.TrimSpace(evt.Content)
	if content == "" {
		return
	}

	// Case 1: Single targeted user (e.g., ProfileRejectedEvent or PaymentSuccessEvent for Candidate X)
	if evt.UserID > 0 {
		dto, err := s.insertRealtimeNotification(ctx, evt.UserID, role, notifType, title, content)
		if err != nil {
			log.Printf("[notification] persist error for user_id=%d: %v", evt.UserID, err)
			return
		}
		if s.hub != nil {
			s.hub.SendToUser(evt.UserID, ws.EventNotificationPayload, dto)
		}
		return
	}

	// Case 2: Explicit list of targeted users (e.g., ExamCancelled or ExamRescheduled across a Session/Period)
	if len(evt.UserIDs) > 0 {
		seen := make(map[int64]struct{}, len(evt.UserIDs))
		for _, uid := range evt.UserIDs {
			if uid <= 0 {
				continue
			}
			if _, dup := seen[uid]; dup {
				continue
			}
			seen[uid] = struct{}{}
			dto, err := s.insertRealtimeNotification(ctx, uid, role, notifType, title, content)
			if err != nil {
				log.Printf("[notification] persist error for batch user_id=%d: %v", uid, err)
				continue
			}
			if s.hub != nil {
				s.hub.SendToUser(uid, ws.EventNotificationPayload, dto)
			}
		}
		return
	}

	// Case 3: Broadcast to all candidates in an Exam Session (e.g., RescheduleExam for Session Y)
	if evt.SessionID > 0 {
		userIDs, err := s.getSessionCandidateAccountIDs(ctx, evt.SessionID)
		if err != nil {
			log.Printf("[notification] lookup session candidates error for session_id=%d: %v", evt.SessionID, err)
			return
		}
		for _, uid := range userIDs {
			dto, err := s.insertRealtimeNotification(ctx, uid, role, notifType, title, content)
			if err != nil {
				log.Printf("[notification] persist error for session_id=%d user_id=%d: %v", evt.SessionID, uid, err)
				continue
			}
			if s.hub != nil {
				s.hub.SendToUser(uid, ws.EventNotificationPayload, dto)
			}
		}
	}
}

func (s *Service) insertRealtimeNotification(ctx context.Context, accountID int64, role, notifType, title, content string) (response.NotificationResponseDTO, error) {
	var e NotificationEntity
	err := scanNotification(s.db.QueryRowContext(ctx, `
		INSERT INTO notifications (
			notification_account_id,
			notification_role,
			notification_type,
			notification_title,
			notification_channel,
			notification_content,
			notification_is_read,
			notification_status
		) VALUES ($1, $2, $3, $4, 'push', $5, FALSE, 'pending')
		RETURNING `+notificationColumns,
		accountID, role, notifType, title, content,
	), &e)
	if err != nil {
		return response.NotificationResponseDTO{}, fmt.Errorf("notification: insert realtime: %w", err)
	}
	return ToNotificationResponseDTO(e), nil
}

func (s *Service) getSessionCandidateAccountIDs(ctx context.Context, sessionID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT c.candidate_account_id
		FROM exam_registrations er
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		                 AND c.candidate_is_deleted = FALSE
		WHERE er.exam_registration_exam_session_id = $1
		  AND er.exam_registration_status IN ('pending_payment', 'confirmed')
		  AND er.exam_registration_is_deleted = FALSE`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CreateNotification inserts a pending notification and returns its DTO (backward-compatible helper).
func (s *Service) CreateNotification(ctx context.Context, accountID int64, channel enum.NotificationChannel, content string) (response.NotificationResponseDTO, error) {
	if !channel.Valid() {
		return response.NotificationResponseDTO{}, fmt.Errorf("invalid notification channel: %s", channel)
	}
	var e NotificationEntity
	err := scanNotification(s.db.QueryRowContext(ctx, `
		INSERT INTO notifications (
			notification_account_id,
			notification_role,
			notification_type,
			notification_title,
			notification_channel,
			notification_content,
			notification_is_read,
			notification_status
		) VALUES ($1, 'candidate', 'SYSTEM', 'Thông báo hệ thống', $2, $3, FALSE, 'pending')
		RETURNING `+notificationColumns,
		accountID, channel, content,
	), &e)
	if err != nil {
		return response.NotificationResponseDTO{}, fmt.Errorf("notification: create: %w", err)
	}
	dto := ToNotificationResponseDTO(e)
	if s.hub != nil {
		s.hub.SendToUser(accountID, ws.EventNotificationPayload, dto)
	}
	return dto, nil
}

// MarkRead marks a notification as read (is_read = TRUE, status = 'sent') for the authenticated owner
// and broadcasts a targeted notification_read event to all active WebSocket tabs of that user.
func (s *Service) MarkRead(ctx context.Context, notificationID, accountID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET notification_is_read = TRUE,
		    notification_status = 'sent',
		    notification_updated_at = NOW()
		WHERE notification_id = $1
		  AND notification_account_id = $2
		  AND notification_is_deleted = FALSE`, notificationID, accountID)
	if err != nil {
		return fmt.Errorf("notification: mark read: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("Không tìm thấy thông báo")
	}
	if s.hub != nil {
		s.hub.SendToUser(accountID, ws.EventNotificationRead, map[string]any{
			"id":      notificationID,
			"user_id": accountID,
			"is_read": true,
		})
	}
	return nil
}

// MarkSent transitions a notification to 'sent' (backward-compatible helper).
func (s *Service) MarkSent(ctx context.Context, notificationID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET notification_is_read = TRUE,
		    notification_status = 'sent',
		    notification_updated_at = NOW()
		WHERE notification_id = $1 AND notification_is_deleted = FALSE`, notificationID)
	if err != nil {
		return fmt.Errorf("notification: mark sent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("Không tìm thấy thông báo")
	}
	return nil
}

// GetMyNotifications returns a user's non-deleted notifications ordered newest first.
// When afterID > 0 is passed (e.g. GET /notifications?after_id=<max_local_id>), only notifications
// with notification_id > afterID are returned for missed-message catchup on reconnect.
func (s *Service) GetMyNotifications(ctx context.Context, accountID int64, afterID ...int64) ([]response.NotificationResponseDTO, error) {
	var minID int64
	if len(afterID) > 0 && afterID[0] > 0 {
		minID = afterID[0]
	}

	var (
		rows *sql.Rows
		err  error
	)
	if minID > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+notificationColumns+`
			FROM notifications
			WHERE notification_account_id = $1
			  AND notification_id > $2
			  AND notification_is_deleted = FALSE
			ORDER BY notification_created_at DESC, notification_id DESC
			LIMIT 50`, accountID, minID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+notificationColumns+`
			FROM notifications
			WHERE notification_account_id = $1 AND notification_is_deleted = FALSE
			ORDER BY notification_created_at DESC, notification_id DESC
			LIMIT 50`, accountID)
	}
	if err != nil {
		return nil, fmt.Errorf("notification: list: %w", err)
	}
	defer rows.Close()
	list := make([]response.NotificationResponseDTO, 0)
	for rows.Next() {
		var e NotificationEntity
		if err := scanNotification(rows, &e); err != nil {
			return nil, fmt.Errorf("notification: scan: %w", err)
		}
		list = append(list, ToNotificationResponseDTO(e))
	}
	return list, rows.Err()
}
