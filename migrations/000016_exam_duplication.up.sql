-- 000016_exam_duplication.up.sql
-- Add support for duplicating individual exam sessions within the same exam period

-- 1. Add session naming, description, and tracing column
ALTER TABLE exam_sessions
    ADD COLUMN IF NOT EXISTS exam_session_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS exam_session_description TEXT,
    ADD COLUMN IF NOT EXISTS exam_session_duplicated_from_id BIGINT REFERENCES exam_sessions (exam_session_id) ON DELETE SET NULL;

-- 2. Allow capacity (slots) to be NULL for draft duplicated sessions
ALTER TABLE exam_sessions
    ALTER COLUMN exam_session_capacity DROP NOT NULL;

-- 3. Adjust capacity check constraint to allow NULL while ensuring positive integer when set
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_capacity_check;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_exam_session_capacity_check
    CHECK (exam_session_capacity IS NULL OR exam_session_capacity > 0);

-- 4. Constraint ck_exam_publish_needs_date_slots:
-- Blocks publishing at DB level if date, slots (capacity), or deadline is NULL
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS chk_exam_sessions_publish_requires_schedule;

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_publish_needs_date_slots;

ALTER TABLE exam_sessions
    ADD CONSTRAINT ck_exam_publish_needs_date_slots
    CHECK (
        exam_session_publication_status = 'draft'
        OR (
            exam_session_date IS NOT NULL
            AND exam_session_capacity IS NOT NULL
            AND exam_session_registration_deadline IS NOT NULL
        )
    );

-- 5. Backfill name for existing sessions
UPDATE exam_sessions es
SET exam_session_name = et.exam_type_name || ' - ' || INITCAP(es.exam_session_shift)
FROM exam_types et
WHERE es.exam_session_exam_type_id = et.exam_type_id
  AND (es.exam_session_name IS NULL OR es.exam_session_name = '');

-- 6. Index for duplicate lineage tracing
CREATE INDEX IF NOT EXISTS idx_exam_sessions_duplicated_from
    ON exam_sessions (exam_session_duplicated_from_id)
    WHERE exam_session_is_deleted = FALSE AND exam_session_duplicated_from_id IS NOT NULL;
