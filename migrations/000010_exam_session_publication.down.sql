DROP INDEX IF EXISTS idx_exam_sessions_publication;

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_publication_status_check,
    DROP COLUMN IF EXISTS exam_session_check_in_at,
    DROP COLUMN IF EXISTS exam_session_publish_at,
    DROP COLUMN IF EXISTS exam_session_publication_status;

ALTER TABLE exam_sessions
    ALTER COLUMN exam_session_exam_room_id SET NOT NULL;
