-- 000020_realtime_notifications.down.sql

DROP INDEX IF EXISTS idx_notifications_user_unread_created;

ALTER TABLE notifications
    DROP COLUMN IF EXISTS notification_is_read,
    DROP COLUMN IF EXISTS notification_title,
    DROP COLUMN IF EXISTS notification_type,
    DROP COLUMN IF EXISTS notification_role,
    ALTER COLUMN notification_channel DROP DEFAULT;

