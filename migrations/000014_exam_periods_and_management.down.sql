-- 000014_exam_periods_and_management.down.sql

ALTER TABLE exam_sessions
    DROP COLUMN IF EXISTS exam_session_exam_period_id;

DROP TRIGGER IF EXISTS trg_exam_periods_updated_at ON exam_periods;
DROP TABLE IF EXISTS exam_periods;
