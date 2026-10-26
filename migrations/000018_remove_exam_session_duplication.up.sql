-- 000018_remove_exam_session_duplication.up.sql
-- Remove the exam-session duplication feature: lineage tracing is no longer used.
-- Only the duplication-specific artifacts are dropped. exam_session_name,
-- exam_session_description, the nullable capacity and every state-machine column
-- remain because other flows depend on them.

DROP INDEX IF EXISTS idx_exam_sessions_duplicated_from;

ALTER TABLE exam_sessions
    DROP COLUMN IF EXISTS exam_session_duplicated_from_id;
