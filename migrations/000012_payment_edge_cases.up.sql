-- =============================================================================
-- Migration 000012: Payment Edge Cases & Evidence Attachments
-- =============================================================================

-- 1. Thêm các cột phục vụ bằng chứng và xử lý hoàn tiền / thanh toán bù vào bảng payments
ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS payment_evidence_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS payment_admin_note TEXT,
    ADD COLUMN IF NOT EXISTS payment_refund_amount NUMERIC(12,0),
    ADD COLUMN IF NOT EXISTS payment_refund_reason TEXT,
    ADD COLUMN IF NOT EXISTS payment_is_top_up BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS payment_parent_payment_id BIGINT REFERENCES payments(payment_id);

CREATE INDEX IF NOT EXISTS idx_payments_parent_id
    ON payments (payment_parent_payment_id)
    WHERE payment_parent_payment_id IS NOT NULL AND payment_is_deleted = FALSE;

-- 2. Bảng quản lý Giao dịch treo (Suspense Accounts / Unmatched Bank Transfers)
CREATE TABLE IF NOT EXISTS payment_suspense_transactions (
    suspense_id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    suspense_order_code              VARCHAR(100),
    suspense_amount                  NUMERIC(12,0) NOT NULL,
    suspense_bank_ref                VARCHAR(100),
    suspense_transfer_content        VARCHAR(255),
    suspense_transfer_time           TIMESTAMPTZ,
    suspense_evidence_urls           JSONB NOT NULL DEFAULT '[]'::jsonb,
    suspense_status                  VARCHAR(20) NOT NULL DEFAULT 'unmatched'
                                     CHECK (suspense_status IN ('unmatched', 'matched', 'refunded', 'ignored')),
    suspense_matched_registration_id BIGINT REFERENCES exam_registrations(exam_registration_id),
    suspense_matched_payment_id      BIGINT REFERENCES payments(payment_id),
    suspense_matched_by_admin_id     BIGINT REFERENCES accounts(account_id),
    suspense_matched_at              TIMESTAMPTZ,
    suspense_admin_note              TEXT,
    suspense_source                  VARCHAR(20) NOT NULL DEFAULT 'manual'
                                     CHECK (suspense_source IN ('webhook', 'manual')),
    suspense_raw_payload             JSONB,
    suspense_is_deleted              BOOLEAN NOT NULL DEFAULT FALSE,
    suspense_deleted_at              TIMESTAMPTZ,
    suspense_created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    suspense_updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_suspense_status ON payment_suspense_transactions (suspense_status, suspense_created_at)
    WHERE suspense_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_suspense_bank_ref ON payment_suspense_transactions (suspense_bank_ref)
    WHERE suspense_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_suspense_order_code ON payment_suspense_transactions (suspense_order_code)
    WHERE suspense_is_deleted = FALSE;

CREATE OR REPLACE TRIGGER trg_suspense_updated_at
    BEFORE UPDATE ON payment_suspense_transactions
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('suspense_updated_at');

-- 3. Cập nhật Stored Procedure xử lý PayOS Webhook hỗ trợ Suspense Account và Hồi phục đơn bị hủy / hết hạn
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
    v_parent_id BIGINT;
    v_is_top_up BOOLEAN;
    v_seat_status VARCHAR(20);
BEGIN
    SELECT payment_id, payment_status, payment_exam_registration_id, payment_amount,
           payment_parent_payment_id, payment_is_top_up
    INTO v_payment_id, v_current_status, v_reg_id, v_payment_amount,
         v_parent_id, v_is_top_up
    FROM payments
    WHERE payment_transaction_ref = v_order_str AND payment_is_deleted = FALSE
    FOR UPDATE;

    INSERT INTO payment_webhook_logs
        (webhook_log_gateway, webhook_log_transaction_ref, webhook_log_payload, webhook_log_payment_id)
    VALUES ('payos', v_order_str, p_raw_payload, v_payment_id);

    -- 3.1. Nếu không tìm thấy payment tương ứng: Tự động đưa vào Giao dịch treo (Suspense Account)
    IF v_payment_id IS NULL THEN
        INSERT INTO payment_suspense_transactions (
            suspense_order_code, suspense_amount, suspense_bank_ref,
            suspense_transfer_content, suspense_status, suspense_source,
            suspense_raw_payload
        ) VALUES (
            v_order_str,
            COALESCE((p_raw_payload->'data'->>'amount')::BIGINT, 0),
            p_gateway_ref,
            COALESCE(p_raw_payload->'data'->>'description', ''),
            'unmatched',
            'webhook',
            p_raw_payload
        );
        RETURN 0;
    END IF;

    -- 3.2. Idempotency guard: nếu đã success hoặc refunded thì bỏ qua
    IF v_current_status IN ('success', 'refunded') THEN
        RETURN -1;
    END IF;

    v_received := COALESCE((p_raw_payload->'data'->>'amount')::BIGINT, 0);

    -- 3.3. Edge Case: Trừ tiền thành công nhưng đơn đã bị hủy / hết hạn (quá 15p nhả ghế)
    IF v_current_status IN ('expired', 'cancelled', 'failed') AND p_is_success THEN
        SELECT exam_seat_status INTO v_seat_status
        FROM exam_seats s
        JOIN exam_registrations er ON er.exam_registration_exam_seat_id = s.exam_seat_id
        WHERE er.exam_registration_id = v_reg_id AND s.exam_seat_is_deleted = FALSE
        FOR UPDATE OF s;

        IF v_seat_status = 'available' THEN
            -- Ghế còn trống -> Tự động kích hoạt lại đơn đăng ký và giữ ghế
            UPDATE exam_seats
            SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
            WHERE exam_seat_id = (SELECT exam_registration_exam_seat_id FROM exam_registrations WHERE exam_registration_id = v_reg_id);

            UPDATE exam_registrations
            SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
            WHERE exam_registration_id = v_reg_id;

            UPDATE payments
            SET payment_status = 'success',
                payment_amount_received = v_received,
                payment_gateway_trans_id = p_gateway_ref,
                payment_paid_at = NOW(),
                payment_admin_note = 'Tự động hồi phục sau khi hết hạn do PayOS đã trừ tiền thành công',
                payment_updated_at = NOW()
            WHERE payment_id = v_payment_id;

            RETURN v_payment_id;
        ELSE
            -- Ghế đã bị người khác lấy -> Đánh dấu hoàn tiền 100%
            UPDATE payments
            SET payment_status = 'refunded',
                payment_amount_received = v_received,
                payment_gateway_trans_id = p_gateway_ref,
                payment_paid_at = NOW(),
                payment_refund_amount = v_received,
                payment_refund_reason = 'Thanh toán thành công trên PayOS nhưng đơn quá hạn và ghế đã bị người khác lấy',
                payment_admin_note = 'Ghế đã bị người khác lấy, tự động chuyển danh sách hoàn tiền 100%',
                payment_updated_at = NOW()
            WHERE payment_id = v_payment_id;

            RETURN v_payment_id;
        END IF;
    END IF;

    IF v_current_status <> 'pending' THEN
        RETURN -1;
    END IF;

    -- 3.4. Kiểm tra số tiền nhận được so với số tiền cần thanh toán
    IF NOT v_is_top_up AND v_received <> v_payment_amount THEN
        UPDATE payments
        SET payment_status = CASE WHEN v_received < v_payment_amount THEN 'partial_paid' ELSE 'amount_mismatch' END,
            payment_amount_received = v_received,
            payment_gateway_trans_id = p_gateway_ref,
            payment_updated_at = NOW()
        WHERE payment_id = v_payment_id;
        RETURN -2;
    END IF;

    -- 3.5. Cập nhật trạng thái thanh toán chuẩn
    v_new_status := CASE WHEN p_is_success THEN 'success' ELSE 'failed' END;
    UPDATE payments
    SET payment_status = v_new_status,
        payment_amount_received = v_received,
        payment_gateway_trans_id = p_gateway_ref,
        payment_paid_at = CASE WHEN p_is_success THEN NOW() ELSE NULL END,
        payment_updated_at = NOW()
    WHERE payment_id = v_payment_id;

    IF p_is_success THEN
        -- Nếu là thanh toán bù (top-up payment), cập nhật payment cha
        IF v_is_top_up AND v_parent_id IS NOT NULL THEN
            UPDATE payments
            SET payment_amount_received = COALESCE(payment_amount_received, 0) + v_received,
                payment_status = 'success',
                payment_paid_at = COALESCE(payment_paid_at, NOW()),
                payment_updated_at = NOW()
            WHERE payment_id = v_parent_id;
        END IF;

        UPDATE exam_registrations
        SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
        WHERE exam_registration_id = v_reg_id
          AND exam_registration_status IN ('pending_payment', 'cancelled')
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
