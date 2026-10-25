-- 000016_exam_duplication.down.sql

DROP INDEX IF EXISTS idx_exam_sessions_duplicated_from;

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_publish_needs_date_slots;

ALTER TABLE exam_sessions
    ADD CONSTRAINT chk_exam_sessions_publish_requires_schedule
    CHECK (
        exam_session_publication_status = 'draft'
        OR (exam_session_date IS NOT NULL AND exam_session_registration_deadline IS NOT NULL)
    );

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_capacity_check;

-- Ensure non-null before restoring constraint
UPDATE exam_sessions SET exam_session_capacity = 30 WHERE exam_session_capacity IS NULL;

ALTER TABLE exam_sessions
    ALTER COLUMN exam_session_capacity SET NOT NULL;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_exam_session_capacity_check
    CHECK (exam_session_capacity > 0);

ALTER TABLE exam_sessions
    DROP COLUMN IF EXISTS exam_session_duplicated_from_id,
    DROP COLUMN IF EXISTS exam_session_description,
    DROP COLUMN IF EXISTS exam_session_name;
