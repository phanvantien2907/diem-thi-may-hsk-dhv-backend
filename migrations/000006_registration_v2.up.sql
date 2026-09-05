-- =============================================================================
-- 006_registration_v2.sql
-- Registration Module v2: waitlist, attendance, transfer support
-- PostgreSQL 16 | Idempotent
-- =============================================================================

-- 1. Add attendance columns to exam_registrations
ALTER TABLE exam_registrations
    ADD COLUMN IF NOT EXISTS exam_registration_is_attended   BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS exam_registration_attended_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS exam_registration_transferred_from_id BIGINT REFERENCES exam_registrations (exam_registration_id);

-- Index for proctors to look up session attendance
CREATE INDEX IF NOT EXISTS idx_exam_reg_session_attended
    ON exam_registrations (exam_registration_exam_session_id, exam_registration_is_attended)
    WHERE exam_registration_is_deleted = FALSE;

-- 2. exam_waitlists table
CREATE TABLE IF NOT EXISTS exam_waitlists (
    exam_waitlist_id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_waitlist_candidate_id     BIGINT      NOT NULL REFERENCES candidates (candidate_id),
    exam_waitlist_exam_session_id  BIGINT      NOT NULL REFERENCES exam_sessions (exam_session_id),
    exam_waitlist_joined_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_waitlist_is_deleted       BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_waitlist_deleted_at       TIMESTAMPTZ,
    exam_waitlist_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_waitlist_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One active waitlist entry per candidate per session
    CONSTRAINT uq_waitlist_candidate_session UNIQUE (exam_waitlist_candidate_id, exam_waitlist_exam_session_id)
);

CREATE INDEX IF NOT EXISTS idx_waitlist_session
    ON exam_waitlists (exam_waitlist_exam_session_id)
    WHERE exam_waitlist_is_deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_waitlist_candidate
    ON exam_waitlists (exam_waitlist_candidate_id)
    WHERE exam_waitlist_is_deleted = FALSE;

CREATE OR REPLACE TRIGGER trg_exam_waitlists_updated_at
    BEFORE UPDATE ON exam_waitlists
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_waitlist_updated_at');

-- 3. fn_transfer_registration: atomic transfer to another session
-- Cancels old seat and books a new seat in target session, updating the existing registration.
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
    -- Lock and validate old registration (ownership + status)
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

    -- Prevent transfer to same session
    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_id = p_registration_id
          AND exam_registration_exam_session_id = p_new_session_id
    ) THEN
        RAISE EXCEPTION 'TRANSFER_SAME_SESSION' USING ERRCODE = 'P0001';
    END IF;

    -- Check no active registration in new session
    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_candidate_id = p_candidate_id
          AND exam_registration_exam_session_id = p_new_session_id
          AND exam_registration_status IN ('pending_payment', 'confirmed')
          AND exam_registration_is_deleted = FALSE
    ) THEN
        RAISE EXCEPTION 'DUPLICATE_REGISTRATION' USING ERRCODE = 'P0001';
    END IF;

    -- Claim a seat in new session (SKIP LOCKED = no queue)
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
        exam_seat_held_until = CASE WHEN v_status = 'confirmed' THEN NULL ELSE now() + make_interval(mins => p_hold_minutes) END,
        exam_seat_updated_at = now()
    FROM picked_seat
    WHERE es.exam_seat_id = picked_seat.exam_seat_id
    RETURNING es.exam_seat_id INTO v_new_seat_id;

    IF v_new_seat_id IS NULL THEN
        RAISE EXCEPTION 'NO_SEAT_AVAILABLE' USING ERRCODE = 'P0001';
    END IF;

    -- Release old seat
    UPDATE exam_seats
    SET exam_seat_status     = 'available',
        exam_seat_held_until = NULL,
        exam_seat_updated_at = now()
    WHERE exam_seat_id = v_old_seat_id;

    -- Update registration (keeps payment intact!)
    UPDATE exam_registrations
    SET exam_registration_exam_session_id = p_new_session_id,
        exam_registration_exam_seat_id = v_new_seat_id,
        exam_registration_updated_at = now()
    WHERE exam_registration_id = p_registration_id;

    RETURN p_registration_id;
END;
$$ LANGUAGE plpgsql;
