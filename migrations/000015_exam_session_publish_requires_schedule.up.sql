-- 000015: Defense-in-depth for the "duplicate exam period" workflow.
--
-- Cloned sessions are created as draft with exam_session_date / registration_deadline = NULL
-- so the admin is forced to re-enter the schedule. This constraint guarantees at the DB level
-- that a session can NEVER leave the draft state (published / scheduled) without a schedule,
-- regardless of which code path (admin API, background publisher, manual SQL) tries it.

ALTER TABLE exam_sessions
    ADD CONSTRAINT chk_exam_sessions_publish_requires_schedule
    CHECK (
        exam_session_publication_status = 'draft'
        OR (exam_session_date IS NOT NULL AND exam_session_registration_deadline IS NOT NULL)
    );
