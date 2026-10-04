ALTER TABLE exam_sessions
    ALTER COLUMN exam_session_exam_room_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS exam_session_publication_status VARCHAR(20) NOT NULL DEFAULT 'published',
    ADD COLUMN IF NOT EXISTS exam_session_publish_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS exam_session_check_in_at TIMESTAMPTZ;

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_publication_status_check,
    ADD CONSTRAINT exam_sessions_publication_status_check
        CHECK (exam_session_publication_status IN ('published', 'draft', 'scheduled'));

CREATE INDEX IF NOT EXISTS idx_exam_sessions_publication
    ON exam_sessions (exam_session_publication_status, exam_session_publish_at)
    WHERE exam_session_is_deleted = FALSE;
