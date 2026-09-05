-- =====================================================================
-- FUNCTION NGHIỆP VỤ: ĐẶT GHẾ THI (concurrency-safe)
-- =====================================================================

-- fn_book_exam_seat: thí sinh bấm "Đăng ký" -> giữ 1 ghế trống trong tối đa
-- p_hold_minutes phút (chờ thanh toán). Dùng FOR UPDATE SKIP LOCKED thay vì
-- khoá nguyên dòng exam_sessions.
CREATE OR REPLACE FUNCTION fn_book_exam_seat(
    p_candidate_id     BIGINT,
    p_exam_session_id  BIGINT,
    p_hold_minutes     INT DEFAULT 15
) RETURNS BIGINT AS $$
DECLARE
    v_seat_id         BIGINT;
    v_registration_id BIGINT;
BEGIN
    -- Chặn đăng ký trùng: 1 thí sinh chỉ có 1 phiếu đăng ký còn hiệu lực / ca thi
    IF EXISTS (
        SELECT 1 FROM exam_registrations
        WHERE exam_registration_candidate_id = p_candidate_id
          AND exam_registration_exam_session_id = p_exam_session_id
          AND exam_registration_status IN ('pending_payment','confirmed')
          AND NOT exam_registration_is_deleted
    ) THEN
        RAISE EXCEPTION 'DUPLICATE_REGISTRATION' USING ERRCODE = 'P0001';
    END IF;

    -- Trái tim của cơ chế concurrency: CTE UPDATE + SKIP LOCKED
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

-- fn_confirm_registration_payment: xác nhận thanh toán -> chốt ghế = booked
CREATE OR REPLACE FUNCTION fn_confirm_registration_payment(
    p_exam_registration_id BIGINT,
    p_amount               NUMERIC,
    p_method                VARCHAR,
    p_transaction_ref       VARCHAR
) RETURNS BIGINT AS $$
DECLARE
    v_seat_id    BIGINT;
    v_status     VARCHAR(20);
    v_held_until TIMESTAMPTZ;
    v_payment_id BIGINT;
BEGIN
    -- Khoá đúng phiếu đăng ký đang xử lý, tránh double payment
    SELECT exam_registration_exam_seat_id, exam_registration_status
    INTO v_seat_id, v_status
    FROM exam_registrations
    WHERE exam_registration_id = p_exam_registration_id
    FOR UPDATE;

    IF v_status IS NULL THEN
        RAISE EXCEPTION 'REGISTRATION_NOT_FOUND' USING ERRCODE = 'P0001';
    ELSIF v_status <> 'pending_payment' THEN
        RAISE EXCEPTION 'REGISTRATION_NOT_PAYABLE' USING ERRCODE = 'P0001';
    END IF;

    SELECT exam_seat_held_until INTO v_held_until
    FROM exam_seats WHERE exam_seat_id = v_seat_id FOR UPDATE;

    IF v_held_until < now() THEN
        RAISE EXCEPTION 'SEAT_HOLD_EXPIRED' USING ERRCODE = 'P0001';
    END IF;

    INSERT INTO payments (
        payment_exam_registration_id, payment_amount, payment_method,
        payment_transaction_ref, payment_status, payment_paid_at
    ) VALUES (
        p_exam_registration_id, p_amount, p_method, p_transaction_ref, 'success', now()
    ) RETURNING payment_id INTO v_payment_id;

    UPDATE exam_registrations
    SET exam_registration_status = 'confirmed',
        exam_registration_updated_at = now()
    WHERE exam_registration_id = p_exam_registration_id;

    UPDATE exam_seats
    SET exam_seat_status = 'booked', 
        exam_seat_held_until = NULL,
        exam_seat_updated_at = now()
    WHERE exam_seat_id = v_seat_id;

    RETURN v_payment_id;
END;
$$ LANGUAGE plpgsql;

-- fn_release_expired_seat_holds: job chạy định kỳ trả lại ghế bị giữ quá hạn.
CREATE OR REPLACE FUNCTION fn_release_expired_seat_holds() RETURNS INT AS $$
DECLARE
    v_count INT;
BEGIN
    WITH expired_seats AS (
        SELECT exam_seat_id
        FROM exam_seats
        WHERE exam_seat_status = 'held'
          AND exam_seat_held_until < now()
        FOR UPDATE SKIP LOCKED
    ),
    cancelled_regs AS (
        UPDATE exam_registrations r
        SET exam_registration_status = 'cancelled',
            exam_registration_updated_at = now()
        FROM expired_seats s
        WHERE r.exam_registration_exam_seat_id = s.exam_seat_id
          AND r.exam_registration_status = 'pending_payment'
        RETURNING r.exam_registration_id
    )
    UPDATE exam_seats
    SET exam_seat_status = 'available', 
        exam_seat_held_until = NULL,
        exam_seat_updated_at = now()
    WHERE exam_seat_id IN (SELECT exam_seat_id FROM expired_seats);

    GET DIAGNOSTICS v_count = ROW_COUNT;
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;
