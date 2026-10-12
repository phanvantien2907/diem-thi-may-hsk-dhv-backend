ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS payment_amount_received NUMERIC(12,0);

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_payment_status_check;

ALTER TABLE payments
    ADD CONSTRAINT payments_payment_status_check
    CHECK (payment_status IN (
        'pending', 'success', 'failed', 'refunded', 'expired',
        'cancelled', 'amount_mismatch', 'partial_paid'
    ));

CREATE INDEX IF NOT EXISTS idx_payments_reconciliation
    ON payments (payment_status, payment_created_at, payment_transaction_ref, payment_gateway_trans_id)
    WHERE payment_is_deleted = FALSE;

CREATE OR REPLACE FUNCTION fn_process_payos_webhook(
    p_order_code BIGINT,
    p_gateway_ref VARCHAR,
    p_is_success BOOLEAN,
    p_raw_payload JSONB
) RETURNS BIGINT AS $$
DECLARE
    v_payment_id BIGINT;
    v_reg_id BIGINT;
    v_seat_id BIGINT;
    v_current_status VARCHAR(20);
    v_payment_amount BIGINT;
    v_received BIGINT;
    v_new_status VARCHAR(20);
    v_order_str VARCHAR(100) := p_order_code::TEXT;
BEGIN
    SELECT payment_id, payment_status, payment_exam_registration_id, payment_amount
    INTO v_payment_id, v_current_status, v_reg_id, v_payment_amount
    FROM payments
    WHERE payment_transaction_ref = v_order_str AND payment_is_deleted = FALSE
    FOR UPDATE;

    INSERT INTO payment_webhook_logs
        (webhook_log_gateway, webhook_log_transaction_ref, webhook_log_payload, webhook_log_payment_id)
    VALUES ('payos', v_order_str, p_raw_payload, v_payment_id);

    IF v_payment_id IS NULL THEN RETURN 0; END IF;
    IF v_current_status <> 'pending' THEN RETURN -1; END IF;

    v_received := (p_raw_payload->'data'->>'amount')::BIGINT;
    IF v_received <> v_payment_amount THEN
        UPDATE payments
        SET payment_status = CASE WHEN v_received < v_payment_amount THEN 'partial_paid' ELSE 'amount_mismatch' END,
            payment_amount_received = v_received,
            payment_gateway_trans_id = p_gateway_ref,
            payment_updated_at = NOW()
        WHERE payment_id = v_payment_id;
        RETURN -2;
    END IF;

    v_new_status := CASE WHEN p_is_success THEN 'success' ELSE 'failed' END;
    UPDATE payments
    SET payment_status = v_new_status,
        payment_amount_received = v_received,
        payment_gateway_trans_id = p_gateway_ref,
        payment_paid_at = CASE WHEN p_is_success THEN NOW() ELSE NULL END,
        payment_updated_at = NOW()
    WHERE payment_id = v_payment_id;

    IF p_is_success THEN
        UPDATE exam_registrations
        SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
        WHERE exam_registration_id = v_reg_id
          AND exam_registration_status = 'pending_payment'
          AND exam_registration_is_deleted = FALSE
        RETURNING exam_registration_exam_seat_id INTO v_seat_id;

        IF v_seat_id IS NOT NULL THEN
            UPDATE exam_seats
            SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
            WHERE exam_seat_id = v_seat_id
              AND exam_seat_status IN ('held', 'available')
              AND exam_seat_is_deleted = FALSE;
        END IF;
    END IF;
    RETURN v_payment_id;
END;
$$ LANGUAGE plpgsql;
