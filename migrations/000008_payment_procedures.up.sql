-- =====================================================================
-- FUNCTION NGHIỆP VỤ: THANH TOÁN PayOS (idempotent, concurrency-safe)
-- =====================================================================

CREATE OR REPLACE FUNCTION fn_process_payos_webhook(
    p_order_code       BIGINT,      
    p_gateway_ref      VARCHAR,     
    p_is_success       BOOLEAN,     
    p_raw_payload      JSONB        
) RETURNS BIGINT AS $$
DECLARE
    v_payment_id     BIGINT;
    v_reg_id         BIGINT;
    v_seat_id        BIGINT;
    v_current_status VARCHAR(20);
    v_payment_amount BIGINT;
    v_new_status     VARCHAR(20);
    v_order_str      VARCHAR(100) := p_order_code::TEXT;
BEGIN
    -- 1. Kiểm tra Idempotency & Lock row thanh toán
    SELECT payment_id, payment_status, payment_exam_registration_id, payment_amount
    INTO v_payment_id, v_current_status, v_reg_id, v_payment_amount
    FROM payments
    WHERE payment_transaction_ref = v_order_str AND payment_is_deleted = FALSE
    FOR UPDATE;

    -- 2. Log webhook ngay lập tức (with v_payment_id if found, NULL if not)
    INSERT INTO payment_webhook_logs (webhook_log_gateway, webhook_log_transaction_ref, webhook_log_payload, webhook_log_payment_id)
    VALUES ('payos', v_order_str, p_raw_payload, v_payment_id);

    -- 3. Bảo vệ Idempotency
    IF v_payment_id IS NULL THEN RETURN 0; END IF;
    IF v_current_status <> 'pending' THEN RETURN -1; END IF;

    -- 3.1. Bảo vệ lệch số tiền
    IF (p_raw_payload->'data'->>'amount')::BIGINT <> v_payment_amount THEN
        -- Có thể rủi ro hacker sửa số tiền hoặc User thanh toán thiếu/dư
        -- Ta từ chối cập nhật thành Success (Giữ pending hoặc đánh dấu Failed)
        -- Để an toàn, văng lỗi hoặc return mã lỗi riêng (VD: -2)
        RETURN -2;
    END IF;

    -- 4. Update payment
    v_new_status := CASE WHEN p_is_success THEN 'success' ELSE 'failed' END;

    UPDATE payments
    SET payment_status = v_new_status,
        payment_gateway_trans_id = p_gateway_ref,
        payment_paid_at = CASE WHEN p_is_success THEN NOW() ELSE NULL END,
        payment_updated_at = NOW()
    WHERE payment_id = v_payment_id;

    -- 4. Xác nhận đăng ký & đặt chỗ (Optimized with RETURNING để tiết kiệm 1 lock)
    IF p_is_success THEN
        UPDATE exam_registrations
        SET exam_registration_status = 'confirmed',
            exam_registration_updated_at = NOW()
        WHERE exam_registration_id = v_reg_id
          AND exam_registration_status = 'pending_payment'
          AND exam_registration_is_deleted = FALSE
        RETURNING exam_registration_exam_seat_id INTO v_seat_id;

        IF v_seat_id IS NOT NULL THEN
            UPDATE exam_seats
            SET exam_seat_status = 'booked',
                exam_seat_held_until = NULL,
                exam_seat_updated_at = NOW()
            WHERE exam_seat_id = v_seat_id
              AND exam_seat_status IN ('held', 'available');
        END IF;
    END IF;

    RETURN v_payment_id;
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION fn_admin_confirm_payment(
    p_payment_id BIGINT
) RETURNS BIGINT AS $$
DECLARE
    v_reg_id         BIGINT;
    v_seat_id        BIGINT;
    v_current_status VARCHAR(20);
BEGIN
    SELECT payment_status, payment_exam_registration_id
    INTO v_current_status, v_reg_id
    FROM payments
    WHERE payment_id = p_payment_id AND payment_is_deleted = FALSE
    FOR UPDATE;

    IF v_current_status IS NULL THEN RAISE EXCEPTION 'PAYMENT_NOT_FOUND' USING ERRCODE = 'P0001'; END IF;
    IF v_current_status <> 'pending' THEN RAISE EXCEPTION 'PAYMENT_NOT_PENDING::%', v_current_status USING ERRCODE = 'P0001'; END IF;

    UPDATE payments
    SET payment_status = 'success', payment_paid_at = NOW(), payment_updated_at = NOW()
    WHERE payment_id = p_payment_id;

    UPDATE exam_registrations
    SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
    WHERE exam_registration_id = v_reg_id
      AND exam_registration_status = 'pending_payment'
      AND exam_registration_is_deleted = FALSE
    RETURNING exam_registration_exam_seat_id INTO v_seat_id;

    IF v_seat_id IS NOT NULL THEN
        UPDATE exam_seats
        SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
        WHERE exam_seat_id = v_seat_id AND exam_seat_status IN ('held', 'available');
    END IF;

    RETURN p_payment_id;
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION fn_admin_cancel_payment(
    p_payment_id BIGINT
) RETURNS VARCHAR AS $$
DECLARE
    v_current_status VARCHAR(20);
    v_new_status     VARCHAR(20);
BEGIN
    SELECT payment_status INTO v_current_status
    FROM payments WHERE payment_id = p_payment_id AND payment_is_deleted = FALSE FOR UPDATE;

    IF v_current_status IS NULL THEN RAISE EXCEPTION 'PAYMENT_NOT_FOUND' USING ERRCODE = 'P0001'; END IF;

    v_new_status := CASE 
        WHEN v_current_status = 'pending' THEN 'failed'
        WHEN v_current_status = 'success' THEN 'refunded'
        ELSE NULL
    END;

    IF v_new_status IS NULL THEN
        RAISE EXCEPTION 'PAYMENT_NOT_CANCELLABLE::%', v_current_status USING ERRCODE = 'P0001';
    END IF;

    UPDATE payments SET payment_status = v_new_status, payment_updated_at = NOW()
    WHERE payment_id = p_payment_id;

    RETURN v_new_status;
END;
$$ LANGUAGE plpgsql;
