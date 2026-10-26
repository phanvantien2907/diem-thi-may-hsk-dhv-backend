-- 000017_exam_status_state_machine.down.sql
-- Khôi phục vòng đời trạng thái cũ của exam_periods và exam_sessions.

-- ---------------------------------------------------------------------------
-- 1. Khôi phục fn_book_exam_seat (không kiểm tra trạng thái hiệu lực, không FOR SHARE).
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_book_exam_seat(
    p_candidate_id     BIGINT,
    p_exam_session_id  BIGINT,
    p_hold_minutes     INT DEFAULT 15
) RETURNS BIGINT AS $$
DECLARE
    v_seat_id         BIGINT;
    v_registration_id BIGINT;
BEGIN
    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_candidate_id = p_candidate_id
          AND exam_registration_exam_session_id = p_exam_session_id
          AND exam_registration_status IN ('pending_payment','confirmed')
          AND NOT exam_registration_is_deleted
    ) THEN
        RAISE EXCEPTION 'DUPLICATE_REGISTRATION' USING ERRCODE = 'P0001';
    END IF;

    WITH picked_seat AS (
        SELECT exam_seat_id
        FROM exam_seats
        WHERE exam_seat_exam_session_id = p_exam_session_id
          AND exam_seat_status = 'available'
          AND NOT exam_seat_is_deleted
        ORDER BY exam_seat_number
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    UPDATE exam_seats es
    SET exam_seat_status = 'held',
        exam_seat_held_until = now() + make_interval(mins => p_hold_minutes),
        exam_seat_updated_at = now()
    FROM picked_seat
    WHERE es.exam_seat_id = picked_seat.exam_seat_id
    RETURNING es.exam_seat_id INTO v_seat_id;

    IF v_seat_id IS NULL THEN
        RAISE EXCEPTION 'NO_SEAT_AVAILABLE' USING ERRCODE = 'P0001';
    END IF;

    INSERT INTO exam_registrations (
        exam_registration_candidate_id,
        exam_registration_exam_session_id,
        exam_registration_exam_seat_id,
        exam_registration_status
    ) VALUES (
        p_candidate_id, p_exam_session_id, v_seat_id, 'pending_payment'
    ) RETURNING exam_registration_id INTO v_registration_id;

    RETURN v_registration_id;
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS fn_assert_session_accepts_registration(BIGINT, BIGINT);

-- Khôi phục fn_transfer_registration gốc (000006) — không kiểm tra trạng thái ca thi đích.
CREATE OR REPLACE FUNCTION fn_transfer_registration(
    p_registration_id     BIGINT,
    p_new_session_id      BIGINT,
    p_candidate_id        BIGINT,
    p_hold_minutes        INT DEFAULT 15
) RETURNS BIGINT AS $$
DECLARE
    v_old_seat_id         BIGINT;
    v_status              VARCHAR(20);
    v_new_seat_id         BIGINT;
BEGIN
    SELECT exam_registration_exam_seat_id, exam_registration_status
    INTO v_old_seat_id, v_status
    FROM exam_registrations
    WHERE exam_registration_id = p_registration_id
      AND exam_registration_candidate_id = p_candidate_id
      AND exam_registration_is_deleted = FALSE
    FOR UPDATE;

    IF v_old_seat_id IS NULL THEN
        RAISE EXCEPTION 'REGISTRATION_NOT_FOUND' USING ERRCODE = 'P0001';
    END IF;
    IF v_status NOT IN ('pending_payment', 'confirmed') THEN
        RAISE EXCEPTION 'REGISTRATION_NOT_TRANSFERABLE' USING ERRCODE = 'P0001';
    END IF;

    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_id = p_registration_id
          AND exam_registration_exam_session_id = p_new_session_id
    ) THEN
        RAISE EXCEPTION 'TRANSFER_SAME_SESSION' USING ERRCODE = 'P0001';
    END IF;

    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_candidate_id = p_candidate_id
          AND exam_registration_exam_session_id = p_new_session_id
          AND exam_registration_status IN ('pending_payment', 'confirmed')
          AND exam_registration_is_deleted = FALSE
    ) THEN
        RAISE EXCEPTION 'DUPLICATE_REGISTRATION' USING ERRCODE = 'P0001';
    END IF;

    WITH picked_seat AS (
        SELECT exam_seat_id
        FROM exam_seats
        WHERE exam_seat_exam_session_id = p_new_session_id
          AND exam_seat_status = 'available'
          AND exam_seat_is_deleted = FALSE
        ORDER BY exam_seat_number
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    UPDATE exam_seats es
    SET exam_seat_status     = CASE WHEN v_status = 'confirmed' THEN 'booked' ELSE 'held' END,
        exam_seat_held_until = CASE WHEN v_status = 'confirmed' THEN NULL
                                    ELSE now() + make_interval(mins => p_hold_minutes) END,
        exam_seat_updated_at = now()
    FROM picked_seat
    WHERE es.exam_seat_id = picked_seat.exam_seat_id
    RETURNING es.exam_seat_id INTO v_new_seat_id;

    IF v_new_seat_id IS NULL THEN
        RAISE EXCEPTION 'NO_SEAT_AVAILABLE' USING ERRCODE = 'P0001';
    END IF;

    UPDATE exam_seats
    SET exam_seat_status     = 'available',
        exam_seat_held_until = NULL,
        exam_seat_updated_at = now()
    WHERE exam_seat_id = v_old_seat_id;

    UPDATE exam_registrations
    SET exam_registration_exam_session_id = p_new_session_id,
        exam_registration_exam_seat_id = v_new_seat_id,
        exam_registration_updated_at = now()
    WHERE exam_registration_id = p_registration_id;

    RETURN p_registration_id;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- 2. Ca thi: khôi phục trạng thái đơn giản open|closed|cancelled.
-- ---------------------------------------------------------------------------
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_session_publish_needs_schedule,
    DROP CONSTRAINT IF EXISTS ck_exam_session_closed_reason_required;

DROP INDEX IF EXISTS idx_exam_sessions_status_reason;
DROP INDEX IF EXISTS idx_exam_sessions_effective_status;

ALTER TABLE exam_sessions
    ADD COLUMN IF NOT EXISTS exam_session_publication_status VARCHAR(20) NOT NULL DEFAULT 'published';

UPDATE exam_sessions
SET exam_session_publication_status = CASE
        WHEN exam_session_status = 'draft' THEN 'draft'
        WHEN exam_session_status = 'scheduled' THEN 'scheduled'
        ELSE 'published'
    END
WHERE exam_session_publication_status IS DISTINCT FROM 'published'
   OR exam_session_status IN ('draft', 'scheduled');

UPDATE exam_sessions
SET exam_session_status = CASE
        WHEN exam_session_status IN ('draft', 'scheduled') THEN 'open'
        WHEN exam_session_status = 'completed' THEN 'closed'
        ELSE exam_session_status
    END,
    exam_session_updated_at = NOW()
WHERE exam_session_status IN ('draft', 'scheduled', 'completed');

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_status_check,
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_closed_reason_check;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_exam_session_status_check
    CHECK (exam_session_status IN ('open', 'closed', 'cancelled'));

ALTER TABLE exam_sessions
    DROP COLUMN IF EXISTS exam_session_closed_reason;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_publication_status_check
    CHECK (exam_session_publication_status IN ('published', 'draft', 'scheduled'));

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

CREATE INDEX IF NOT EXISTS idx_exam_sessions_publication
    ON exam_sessions (exam_session_publication_status, exam_session_publish_at)
    WHERE exam_session_is_deleted = FALSE;

-- ---------------------------------------------------------------------------
-- 3. Đợt thi: khôi phục trạng thái 'rescheduled' và bỏ cột lưu vết dời lịch.
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_exam_periods_effective_status;

ALTER TABLE exam_periods
    DROP CONSTRAINT IF EXISTS exam_periods_exam_period_status_check;

ALTER TABLE exam_periods
    ADD CONSTRAINT exam_periods_exam_period_status_check
    CHECK (exam_period_status IN ('draft', 'published', 'registration_open',
                                  'registration_closed', 'rescheduled', 'cancelled', 'completed'));

ALTER TABLE exam_periods
    DROP COLUMN IF EXISTS exam_period_original_date,
    DROP COLUMN IF EXISTS exam_period_reschedule_count;
