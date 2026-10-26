-- 000018_remove_exam_session_duplication.down.sql
-- Restore the exam-session duplication lineage column and its partial index.

ALTER TABLE exam_sessions
    ADD COLUMN IF NOT EXISTS exam_session_duplicated_from_id BIGINT
        REFERENCES exam_sessions (exam_session_id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_exam_sessions_duplicated_from
    ON exam_sessions (exam_session_duplicated_from_id)
    WHERE exam_session_is_deleted = FALSE AND exam_session_duplicated_from_id IS NOT NULL;
