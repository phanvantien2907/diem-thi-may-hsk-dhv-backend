ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS chk_exam_sessions_publish_requires_schedule;
