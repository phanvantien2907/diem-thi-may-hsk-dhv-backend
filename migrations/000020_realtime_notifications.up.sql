-- 000020_realtime_notifications.up.sql
-- Mở rộng bảng notifications hiện hữu để hỗ trợ phân hệ Thông báo Thời gian thực (WebSocket & Event-Driven)
-- mà vẫn tương thích ngược 100% với các câu lệnh INSERT hiện có trong hệ thống.

ALTER TABLE notifications
    ALTER COLUMN notification_channel SET DEFAULT 'push',
    ADD COLUMN IF NOT EXISTS notification_role    VARCHAR(20)  NOT NULL DEFAULT 'candidate',
    ADD COLUMN IF NOT EXISTS notification_type    VARCHAR(50)  NOT NULL DEFAULT 'SYSTEM',
    ADD COLUMN IF NOT EXISTS notification_title   VARCHAR(255) NOT NULL DEFAULT 'Thông báo hệ thống',
    ADD COLUMN IF NOT EXISTS notification_is_read BOOLEAN      NOT NULL DEFAULT FALSE;

-- Đồng bộ trạng thái đã đọc cho các bản ghi lịch sử đã gửi
UPDATE notifications
SET notification_is_read = TRUE
WHERE notification_status = 'sent'
  AND notification_is_read = FALSE;

CREATE INDEX IF NOT EXISTS idx_notifications_user_unread_created
    ON notifications (notification_account_id, notification_is_read, notification_created_at DESC)
    WHERE notification_is_deleted = FALSE;

