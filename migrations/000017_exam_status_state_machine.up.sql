-- 000017_exam_status_state_machine.up.sql
-- Chuẩn hoá vòng đời trạng thái của Đợt thi (exam_periods) và Ca thi (exam_sessions).
--
--   Đợt thi : draft -> published -> registration_open -> registration_closed -> completed
--             (+ nhánh cancelled)
--   Ca thi  : draft -> scheduled -> open -> closed -> completed
--             (+ nhánh cancelled)
--
-- "Đã đóng" và "Đã hết hạn" là hai cách hiển thị của cùng một trạng thái `closed`,
-- phân biệt bằng exam_session_closed_reason (manual | full | deadline | period_closed).
--
-- Cột trạng thái chỉ phục vụ hiển thị; trạng thái hiệu lực luôn được tính lại từ now()
-- và các mốc thời gian ở tầng ứng dụng (internal/exam/statemachine.go).

-- ---------------------------------------------------------------------------
-- 1. Đợt thi: lưu vết dời lịch, bỏ trạng thái 'rescheduled'.
-- ---------------------------------------------------------------------------
ALTER TABLE exam_periods
    ADD COLUMN IF NOT EXISTS exam_period_original_date DATE,
    ADD COLUMN IF NOT EXISTS exam_period_reschedule_count INT NOT NULL DEFAULT 0;

UPDATE exam_periods
SET exam_period_status = CASE
        WHEN exam_period_end_date IS NOT NULL AND exam_period_end_date < CURRENT_DATE THEN 'completed'
        WHEN exam_period_registration_deadline IS NOT NULL
             AND exam_period_registration_deadline <= NOW() THEN 'registration_closed'
        WHEN exam_period_registration_start IS NOT NULL
             AND exam_period_registration_start > NOW() THEN 'published'
        ELSE 'registration_open'
    END,
    exam_period_updated_at = NOW()
WHERE exam_period_status = 'rescheduled';

ALTER TABLE exam_periods
    DROP CONSTRAINT IF EXISTS exam_periods_exam_period_status_check;

ALTER TABLE exam_periods
    ADD CONSTRAINT exam_periods_exam_period_status_check
    CHECK (exam_period_status IN ('draft', 'published', 'registration_open',
                                  'registration_closed', 'completed', 'cancelled'));

CREATE INDEX IF NOT EXISTS idx_exam_periods_effective_status
    ON exam_periods (exam_period_status, exam_period_registration_start, exam_period_registration_deadline)
    WHERE exam_period_is_deleted = FALSE;

-- ---------------------------------------------------------------------------
-- 2. Ca thi: gộp trạng thái phát hành (publication) vào một vòng đời duy nhất.
-- ---------------------------------------------------------------------------
ALTER TABLE exam_sessions
    ADD COLUMN IF NOT EXISTS exam_session_closed_reason VARCHAR(20);

-- Phải bỏ constraint cũ TRƯỚC khi backfill: constraint cũ chỉ cho phép
-- open|closed|cancelled nên mọi UPDATE sang draft/scheduled sẽ vi phạm.
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_status_check,
    DROP CONSTRAINT IF EXISTS exam_sessions_status_check;

-- Backfill chỉ chạy khi cột publication_status còn tồn tại. Nhờ vậy file có thể
-- được chạy lại (replay) trên schema đã migrate mà không lỗi
-- "column exam_session_publication_status does not exist".
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'exam_sessions'
          AND column_name = 'exam_session_publication_status'
    ) THEN
        UPDATE exam_sessions
        SET exam_session_status = 'draft',
            exam_session_updated_at = NOW()
        WHERE exam_session_status <> 'cancelled'
          AND exam_session_publication_status = 'draft';

        UPDATE exam_sessions
        SET exam_session_status = 'scheduled',
            exam_session_updated_at = NOW()
        WHERE exam_session_status <> 'cancelled'
          AND exam_session_publication_status = 'scheduled';
    END IF;
END $$;

UPDATE exam_sessions
SET exam_session_closed_reason = 'manual'
WHERE exam_session_status = 'closed'
  AND exam_session_closed_reason IS NULL;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_exam_session_status_check
    CHECK (exam_session_status IN ('draft', 'scheduled', 'open', 'closed', 'completed', 'cancelled'));

ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS exam_sessions_exam_session_closed_reason_check;

ALTER TABLE exam_sessions
    ADD CONSTRAINT exam_sessions_exam_session_closed_reason_check
    CHECK (exam_session_closed_reason IS NULL
           OR exam_session_closed_reason IN ('manual', 'full', 'deadline', 'period_closed'));

-- Lý do đóng là bắt buộc khi và chỉ khi ca thi ở trạng thái closed.
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_session_closed_reason_required;

ALTER TABLE exam_sessions
    ADD CONSTRAINT ck_exam_session_closed_reason_required
    CHECK ((exam_session_status = 'closed') = (exam_session_closed_reason IS NOT NULL));

-- Bỏ cột trạng thái phát hành (đã gộp vào exam_session_status).
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_publish_needs_date_slots,
    DROP CONSTRAINT IF EXISTS chk_exam_sessions_publish_requires_schedule,
    DROP CONSTRAINT IF EXISTS exam_sessions_publication_status_check;

DROP INDEX IF EXISTS idx_exam_sessions_publication;

ALTER TABLE exam_sessions
    DROP COLUMN IF EXISTS exam_session_publication_status;

-- Ca thi đã rời trạng thái nháp/huỷ thì bắt buộc phải có lịch thi và sĩ số.
ALTER TABLE exam_sessions
    DROP CONSTRAINT IF EXISTS ck_exam_session_publish_needs_schedule;

ALTER TABLE exam_sessions
    ADD CONSTRAINT ck_exam_session_publish_needs_schedule
    CHECK (
        exam_session_status IN ('draft', 'cancelled')
        OR (
            exam_session_date IS NOT NULL
            AND exam_session_capacity IS NOT NULL
            AND exam_session_registration_deadline IS NOT NULL
        )
    );

CREATE INDEX IF NOT EXISTS idx_exam_sessions_status_reason
    ON exam_sessions (exam_session_status, exam_session_closed_reason)
    WHERE exam_session_is_deleted = FALSE;

CREATE INDEX IF NOT EXISTS idx_exam_sessions_effective_status
    ON exam_sessions (exam_session_status, exam_session_registration_deadline, exam_session_publish_at)
    WHERE exam_session_is_deleted = FALSE;

-- ---------------------------------------------------------------------------
-- 3. Kiểm tra trạng thái hiệu lực + khoá dòng ca thi bằng FOR SHARE.
--    Lệnh đóng/mở đăng ký của admin ghi vào cùng dòng đó nên hai bên tuần tự hoá,
--    không ai lọt vào đúng khoảnh khắc admin bấm đóng.
--
--    Một hàm guard dùng chung cho cả đặt chỗ mới và chuyển ca để hai luồng này
--    không bao giờ lệch quy tắc.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_assert_session_accepts_registration(
    p_exam_session_id  BIGINT,
    p_candidate_id     BIGINT
) RETURNS VOID AS $$
DECLARE
    v_status           VARCHAR(20);
    v_closed_reason    VARCHAR(20);
    v_deadline         TIMESTAMPTZ;
    v_date             DATE;
    v_publish_at       TIMESTAMPTZ;
    v_capacity         INT;
    v_period_id        BIGINT;
    v_period_status    VARCHAR(30);
    v_period_start     TIMESTAMPTZ;
    v_period_deadline  TIMESTAMPTZ;
    v_period_end_date  DATE;
    v_taken            INT;
BEGIN
    -- Khoá chia sẻ dòng ca thi (giữ tới hết transaction).
    SELECT exam_session_status, exam_session_closed_reason, exam_session_registration_deadline,
           exam_session_date, exam_session_publish_at, exam_session_capacity,
           exam_session_exam_period_id
    INTO v_status, v_closed_reason, v_deadline, v_date, v_publish_at, v_capacity, v_period_id
    FROM exam_sessions
    WHERE exam_session_id = p_exam_session_id
      AND exam_session_is_deleted = FALSE
    FOR SHARE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'EXAM_SESSION_NOT_FOUND' USING ERRCODE = 'P0001';
    END IF;

    IF v_status = 'cancelled' THEN
        RAISE EXCEPTION 'EXAM_SESSION_CANCELLED' USING ERRCODE = 'P0001';
    END IF;

    IF v_status = 'completed' OR (v_date IS NOT NULL AND v_date < CURRENT_DATE) THEN
        RAISE EXCEPTION 'EXAM_SESSION_COMPLETED' USING ERRCODE = 'P0001';
    END IF;

    IF v_status = 'draft' THEN
        RAISE EXCEPTION 'EXAM_SESSION_NOT_PUBLISHED' USING ERRCODE = 'P0001';
    END IF;

    -- Ca hẹn giờ chỉ được coi là công khai khi đã tới giờ công khai.
    IF v_status = 'scheduled' AND (v_publish_at IS NULL OR v_publish_at > NOW()) THEN
        RAISE EXCEPTION 'EXAM_SESSION_NOT_PUBLISHED' USING ERRCODE = 'P0001';
    END IF;

    -- Trạng thái hiệu lực của đợt thi cha.
    IF v_period_id IS NOT NULL THEN
        SELECT exam_period_status, exam_period_registration_start,
               exam_period_registration_deadline, exam_period_end_date
        INTO v_period_status, v_period_start, v_period_deadline, v_period_end_date
        FROM exam_periods
        WHERE exam_period_id = v_period_id
          AND exam_period_is_deleted = FALSE
        FOR SHARE;

        IF NOT FOUND OR v_period_status = 'cancelled' THEN
            RAISE EXCEPTION 'EXAM_PERIOD_CANCELLED' USING ERRCODE = 'P0001';
        END IF;

        IF v_period_status IN ('draft', 'published', 'registration_closed') THEN
            RAISE EXCEPTION 'REGISTRATION_CLOSED' USING ERRCODE = 'P0001';
        END IF;

        IF v_period_status = 'completed'
           OR (v_period_end_date IS NOT NULL AND v_period_end_date < CURRENT_DATE) THEN
            RAISE EXCEPTION 'EXAM_SESSION_COMPLETED' USING ERRCODE = 'P0001';
        END IF;

        IF v_period_deadline IS NOT NULL AND v_period_deadline <= NOW() THEN
            RAISE EXCEPTION 'REGISTRATION_CLOSED' USING ERRCODE = 'P0001';
        END IF;

        IF v_period_start IS NOT NULL AND v_period_start > NOW() THEN
            RAISE EXCEPTION 'REGISTRATION_CLOSED' USING ERRCODE = 'P0001';
        END IF;
    END IF;

    IF v_status = 'closed' THEN
        IF v_closed_reason = 'full' THEN
            RAISE EXCEPTION 'NO_SEAT_AVAILABLE' USING ERRCODE = 'P0001';
        END IF;
        RAISE EXCEPTION 'REGISTRATION_CLOSED' USING ERRCODE = 'P0001';
    END IF;

    IF v_deadline IS NOT NULL AND v_deadline <= NOW() THEN
        RAISE EXCEPTION 'REGISTRATION_CLOSED' USING ERRCODE = 'P0001';
    END IF;

    -- Chặn đăng ký trùng: 1 thí sinh chỉ có 1 phiếu còn hiệu lực / ca thi.
    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_candidate_id = p_candidate_id
          AND exam_registration_exam_session_id = p_exam_session_id
          AND exam_registration_status IN ('pending_payment','confirmed')
          AND NOT exam_registration_is_deleted
    ) THEN
        RAISE EXCEPTION 'DUPLICATE_REGISTRATION' USING ERRCODE = 'P0001';
    END IF;

    -- Sĩ số hiệu lực (không tin cột trạng thái đã lưu).
    IF v_capacity IS NOT NULL THEN
        SELECT COUNT(*) INTO v_taken
        FROM exam_registrations
        WHERE exam_registration_exam_session_id = p_exam_session_id
          AND exam_registration_status IN ('pending_payment','confirmed')
          AND NOT exam_registration_is_deleted;

        IF v_taken >= v_capacity THEN
            RAISE EXCEPTION 'NO_SEAT_AVAILABLE' USING ERRCODE = 'P0001';
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- 4. Đặt chỗ: guard + CTE UPDATE ... FOR UPDATE SKIP LOCKED.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_book_exam_seat(
    p_candidate_id     BIGINT,
    p_exam_session_id  BIGINT,
    p_hold_minutes     INT DEFAULT 15
) RETURNS BIGINT AS $$
DECLARE
    v_seat_id          BIGINT;
    v_registration_id  BIGINT;
BEGIN
    -- Guard dùng chung: khoá ca thi bằng FOR SHARE và kiểm tra trạng thái hiệu lực.
    PERFORM fn_assert_session_accepts_registration(p_exam_session_id, p_candidate_id);

    -- Trái tim của cơ chế concurrency: CTE UPDATE + SKIP LOCKED.
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

-- ---------------------------------------------------------------------------
-- 5. Chuyển ca: áp dụng đúng guard như đặt chỗ mới cho ca thi đích.
-- ---------------------------------------------------------------------------
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
    -- Khoá và kiểm tra phiếu đăng ký cũ (quyền sở hữu + trạng thái).
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

    -- Guard dùng chung: ca thi đích phải đang thực sự nhận đăng ký.
    PERFORM fn_assert_session_accepts_registration(p_new_session_id, p_candidate_id);

    -- Giữ ghế ở ca thi đích.
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

    -- Trả ghế cũ.
    UPDATE exam_seats
    SET exam_seat_status     = 'available',
        exam_seat_held_until = NULL,
        exam_seat_updated_at = now()
    WHERE exam_seat_id = v_old_seat_id;

    -- Cập nhật phiếu đăng ký (giữ nguyên thanh toán).
    UPDATE exam_registrations
    SET exam_registration_exam_session_id = p_new_session_id,
        exam_registration_exam_seat_id = v_new_seat_id,
        exam_registration_updated_at = now()
    WHERE exam_registration_id = p_registration_id;

    RETURN p_registration_id;
END;
$$ LANGUAGE plpgsql;
