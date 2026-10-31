-- =============================================================================
-- Migration 000019: Candidate Profile Approval Hardening, Duplicate Merge
--                   Procedure & Account Deactivation Lifecycle
-- =============================================================================

-- 1. Mở rộng ràng buộc trạng thái tài khoản (accounts.account_status) để hỗ trợ 'disabled'
ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_account_status_check;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_account_status_check
    CHECK (account_status IN ('active', 'locked', 'pending', 'disabled'));

-- 2. Bổ sung các cột phục vụ Soft Lock (5 phút) và Proof-of-View Gatekeeping cho hồ sơ thí sinh
ALTER TABLE candidates
    ADD COLUMN IF NOT EXISTS candidate_review_locked_by BIGINT REFERENCES accounts (account_id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS candidate_review_locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS candidate_last_viewed_by BIGINT REFERENCES accounts (account_id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS candidate_last_viewed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_candidates_review_lock
    ON candidates (candidate_id, candidate_review_locked_until)
    WHERE candidate_deleted_at IS NULL;

-- 3. Index tăng tốc kiểm tra trùng số giấy tờ tùy thân (CCCD / Hộ chiếu)
CREATE INDEX IF NOT EXISTS idx_candidate_docs_doc_number_active
    ON candidate_documents (candidate_document_number, candidate_document_candidate_id)
    WHERE candidate_document_deleted_at IS NULL AND candidate_document_is_deleted = FALSE;

-- 4. Stored Function gộp hồ sơ thí sinh trùng lặp (fn_merge_candidate_profiles)
CREATE OR REPLACE FUNCTION fn_merge_candidate_profiles(
    p_primary_candidate_id   BIGINT,
    p_duplicate_candidate_id BIGINT,
    p_actor_account_id       BIGINT,
    p_reason                 TEXT DEFAULT 'Gộp hồ sơ thí sinh trùng lặp'
) RETURNS JSONB AS $$
DECLARE
    v_primary_cand          candidates%ROWTYPE;
    v_dup_cand              candidates%ROWTYPE;
    v_primary_has_doc       BOOLEAN := FALSE;
    v_latest_dup_doc_id     BIGINT;
    v_cancelled_reg_count   INT := 0;
    v_moved_reg_count       INT := 0;
    v_summary               JSONB;
    rec                     RECORD;
BEGIN
    IF p_primary_candidate_id IS NULL OR p_duplicate_candidate_id IS NULL
       OR p_primary_candidate_id = p_duplicate_candidate_id THEN
        RAISE EXCEPTION 'INVALID_MERGE_TARGETS' USING ERRCODE = 'P0001';
    END IF;

    -- Khóa 2 bản ghi candidate theo thứ tự ID tăng dần để chống Deadlock
    IF p_primary_candidate_id < p_duplicate_candidate_id THEN
        SELECT * INTO v_primary_cand
        FROM candidates
        WHERE candidate_id = p_primary_candidate_id
          AND candidate_deleted_at IS NULL
          AND candidate_is_deleted = FALSE
        FOR UPDATE;

        SELECT * INTO v_dup_cand
        FROM candidates
        WHERE candidate_id = p_duplicate_candidate_id
          AND candidate_deleted_at IS NULL
          AND candidate_is_deleted = FALSE
        FOR UPDATE;
    ELSE
        SELECT * INTO v_dup_cand
        FROM candidates
        WHERE candidate_id = p_duplicate_candidate_id
          AND candidate_deleted_at IS NULL
          AND candidate_is_deleted = FALSE
        FOR UPDATE;

        SELECT * INTO v_primary_cand
        FROM candidates
        WHERE candidate_id = p_primary_candidate_id
          AND candidate_deleted_at IS NULL
          AND candidate_is_deleted = FALSE
        FOR UPDATE;
    END IF;

    IF v_primary_cand.candidate_id IS NULL THEN
        RAISE EXCEPTION 'PRIMARY_CANDIDATE_NOT_FOUND' USING ERRCODE = 'P0001';
    END IF;

    IF v_dup_cand.candidate_id IS NULL THEN
        RAISE EXCEPTION 'DUPLICATE_CANDIDATE_NOT_FOUND' USING ERRCODE = 'P0001';
    END IF;

    -- 4.1. Kiểm tra xung đột đăng ký thi trong cùng một ca thi (exam_session_id)
    -- Nếu cả 2 hồ sơ đều có đơn 'confirmed' (đã đóng tiền) trong cùng 1 ca thi -> Chặn gộp tự động
    IF EXISTS (
        SELECT 1
        FROM exam_registrations r1
        JOIN exam_registrations r2
          ON r1.exam_registration_exam_session_id = r2.exam_registration_exam_session_id
        WHERE r1.exam_registration_candidate_id = p_primary_candidate_id
          AND r2.exam_registration_candidate_id = p_duplicate_candidate_id
          AND r1.exam_registration_status = 'confirmed'
          AND r2.exam_registration_status = 'confirmed'
          AND r1.exam_registration_deleted_at IS NULL
          AND r2.exam_registration_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'MERGE_CONFLICT_BOTH_CONFIRMED_SAME_SESSION' USING ERRCODE = 'P0001';
    END IF;

    -- 4.2. Nếu cùng ca thi mà hồ sơ chính có đơn ('confirmed' hoặc 'pending_payment')
    -- còn hồ sơ phụ có đơn 'pending_payment' -> Hủy đơn 'pending_payment' của hồ sơ phụ và nhả ghế
    FOR rec IN
        SELECT r_dup.exam_registration_id, r_dup.exam_registration_exam_seat_id
        FROM exam_registrations r_dup
        JOIN exam_registrations r_pri
          ON r_pri.exam_registration_exam_session_id = r_dup.exam_registration_exam_session_id
        WHERE r_pri.exam_registration_candidate_id = p_primary_candidate_id
          AND r_dup.exam_registration_candidate_id = p_duplicate_candidate_id
          AND r_pri.exam_registration_status IN ('confirmed', 'pending_payment')
          AND r_dup.exam_registration_status = 'pending_payment'
          AND r_pri.exam_registration_deleted_at IS NULL
          AND r_dup.exam_registration_deleted_at IS NULL
        FOR UPDATE OF r_dup
    LOOP
        UPDATE exam_registrations
        SET exam_registration_status = 'cancelled',
            exam_registration_updated_at = NOW()
        WHERE exam_registration_id = rec.exam_registration_id;

        UPDATE payments
        SET payment_status = 'cancelled',
            payment_admin_note = COALESCE(payment_admin_note || ' | ', '') || 'Tự động hủy do gộp hồ sơ trùng lặp',
            payment_updated_at = NOW()
        WHERE payment_exam_registration_id = rec.exam_registration_id
          AND payment_status = 'pending'
          AND payment_deleted_at IS NULL;

        UPDATE exam_seats
        SET exam_seat_status = 'available',
            exam_seat_held_until = NULL,
            exam_seat_updated_at = NOW()
        WHERE exam_seat_id = rec.exam_registration_exam_seat_id
          AND exam_seat_status = 'held';

        v_cancelled_reg_count := v_cancelled_reg_count + 1;
    END LOOP;

    -- 4.3. Nếu cùng ca thi mà hồ sơ phụ có đơn 'confirmed' còn hồ sơ chính có đơn 'pending_payment'
    -- -> Hủy đơn 'pending_payment' của hồ sơ chính và nhả ghế để giữ đơn 'confirmed' từ hồ sơ phụ chuyển sang
    FOR rec IN
        SELECT r_pri.exam_registration_id, r_pri.exam_registration_exam_seat_id
        FROM exam_registrations r_pri
        JOIN exam_registrations r_dup
          ON r_dup.exam_registration_exam_session_id = r_pri.exam_registration_exam_session_id
        WHERE r_pri.exam_registration_candidate_id = p_primary_candidate_id
          AND r_dup.exam_registration_candidate_id = p_duplicate_candidate_id
          AND r_pri.exam_registration_status = 'pending_payment'
          AND r_dup.exam_registration_status = 'confirmed'
          AND r_pri.exam_registration_deleted_at IS NULL
          AND r_dup.exam_registration_deleted_at IS NULL
        FOR UPDATE OF r_pri
    LOOP
        UPDATE exam_registrations
        SET exam_registration_status = 'cancelled',
            exam_registration_updated_at = NOW()
        WHERE exam_registration_id = rec.exam_registration_id;

        UPDATE payments
        SET payment_status = 'cancelled',
            payment_admin_note = COALESCE(payment_admin_note || ' | ', '') || 'Tự động hủy do gộp hồ sơ trùng lặp (ưu tiên đơn đã thanh toán)',
            payment_updated_at = NOW()
        WHERE payment_exam_registration_id = rec.exam_registration_id
          AND payment_status = 'pending'
          AND payment_deleted_at IS NULL;

        UPDATE exam_seats
        SET exam_seat_status = 'available',
            exam_seat_held_until = NULL,
            exam_seat_updated_at = NOW()
        WHERE exam_seat_id = rec.exam_registration_exam_seat_id
          AND exam_seat_status = 'held';

        v_cancelled_reg_count := v_cancelled_reg_count + 1;
    END LOOP;

    -- 4.4. Chuyển toàn bộ các đơn đăng ký thi của hồ sơ phụ sang hồ sơ chính
    WITH moved AS (
        UPDATE exam_registrations
        SET exam_registration_candidate_id = p_primary_candidate_id,
            exam_registration_updated_at = NOW()
        WHERE exam_registration_candidate_id = p_duplicate_candidate_id
        RETURNING exam_registration_id
    )
    SELECT COUNT(*) INTO v_moved_reg_count FROM moved;

    -- 4.5. Đồng bộ danh sách chờ (exam_waitlists)
    DELETE FROM exam_waitlists w_dup
    WHERE w_dup.exam_waitlist_candidate_id = p_duplicate_candidate_id
      AND EXISTS (
          SELECT 1 FROM exam_waitlists w_pri
          WHERE w_pri.exam_waitlist_candidate_id = p_primary_candidate_id
            AND w_pri.exam_waitlist_exam_session_id = w_dup.exam_waitlist_exam_session_id
      );

    UPDATE exam_waitlists
    SET exam_waitlist_candidate_id = p_primary_candidate_id,
        exam_waitlist_updated_at = NOW()
    WHERE exam_waitlist_candidate_id = p_duplicate_candidate_id;

    -- 4.6. Xử lý giấy tờ tùy thân (candidate_documents)
    SELECT EXISTS (
        SELECT 1 FROM candidate_documents
        WHERE candidate_document_candidate_id = p_primary_candidate_id
          AND candidate_document_deleted_at IS NULL
          AND candidate_document_is_deleted = FALSE
    ) INTO v_primary_has_doc;

    IF NOT v_primary_has_doc THEN
        -- Nếu hồ sơ chính chưa có giấy tờ nào, giữ lại giấy tờ mới nhất của hồ sơ phụ cho hồ sơ chính
        SELECT candidate_document_id INTO v_latest_dup_doc_id
        FROM candidate_documents
        WHERE candidate_document_candidate_id = p_duplicate_candidate_id
          AND candidate_document_deleted_at IS NULL
          AND candidate_document_is_deleted = FALSE
        ORDER BY candidate_document_created_at DESC, candidate_document_id DESC
        LIMIT 1;
    END IF;

    -- Các giấy tờ còn lại của hồ sơ phụ được chuyển sang hồ sơ chính nhưng đánh dấu soft-delete để lưu lịch sử
    UPDATE candidate_documents
    SET candidate_document_candidate_id = p_primary_candidate_id,
        candidate_document_is_deleted = CASE
            WHEN v_latest_dup_doc_id IS NOT NULL AND candidate_document_id = v_latest_dup_doc_id THEN FALSE
            ELSE TRUE
        END,
        candidate_document_deleted_at = CASE
            WHEN v_latest_dup_doc_id IS NOT NULL AND candidate_document_id = v_latest_dup_doc_id THEN NULL
            ELSE COALESCE(candidate_document_deleted_at, NOW())
        END,
        candidate_document_rejection_reason = CASE
            WHEN v_latest_dup_doc_id IS NOT NULL AND candidate_document_id = v_latest_dup_doc_id THEN candidate_document_rejection_reason
            ELSE COALESCE(candidate_document_rejection_reason || ' | ', '') || 'Merged from candidate #' || p_duplicate_candidate_id
        END,
        candidate_document_updated_at = NOW()
    WHERE candidate_document_candidate_id = p_duplicate_candidate_id;

    -- 4.7. Bồi đắp thông tin còn thiếu trên hồ sơ chính từ hồ sơ phụ
    UPDATE candidates
    SET candidate_full_name_cn        = COALESCE(candidate_full_name_cn, v_dup_cand.candidate_full_name_cn),
        candidate_ethnicity           = COALESCE(candidate_ethnicity, v_dup_cand.candidate_ethnicity),
        candidate_religion            = COALESCE(candidate_religion, v_dup_cand.candidate_religion),
        candidate_birthplace          = COALESCE(candidate_birthplace, v_dup_cand.candidate_birthplace),
        candidate_mother_tongue       = COALESCE(candidate_mother_tongue, v_dup_cand.candidate_mother_tongue),
        candidate_chinese_study_years = COALESCE(candidate_chinese_study_years, v_dup_cand.candidate_chinese_study_years),
        candidate_updated_at          = NOW()
    WHERE candidate_id = p_primary_candidate_id;

    -- Bồi đắp số điện thoại trên tài khoản chính nếu đang trống
    UPDATE accounts a_pri
    SET account_phone = COALESCE(a_pri.account_phone, a_dup.account_phone),
        account_updated_at = NOW()
    FROM accounts a_dup
    WHERE a_pri.account_id = v_primary_cand.candidate_account_id
      AND a_dup.account_id = v_dup_cand.candidate_account_id
      AND (a_pri.account_phone IS NULL OR a_pri.account_phone = '');

    -- 4.8. Soft-delete hồ sơ phụ, vô hiệu hóa tài khoản phụ và thu hồi toàn bộ phiên đăng nhập
    UPDATE candidates
    SET candidate_is_deleted = TRUE,
        candidate_deleted_at = NOW(),
        candidate_review_locked_by = NULL,
        candidate_review_locked_until = NULL,
        candidate_updated_at = NOW()
    WHERE candidate_id = p_duplicate_candidate_id;

    UPDATE accounts
    SET account_status = 'disabled',
        account_is_deleted = TRUE,
        account_deleted_at = NOW(),
        account_updated_at = NOW()
    WHERE account_id = v_dup_cand.candidate_account_id;

    UPDATE auth_sessions
    SET auth_session_revoked_at = NOW()
    WHERE auth_session_account_id = v_dup_cand.candidate_account_id
      AND auth_session_revoked_at IS NULL;

    -- 4.9. Ghi nhật ký kiểm toán (audit_logs)
    v_summary := jsonb_build_object(
        'primary_candidate_id', p_primary_candidate_id,
        'duplicate_candidate_id', p_duplicate_candidate_id,
        'duplicate_account_id', v_dup_cand.candidate_account_id,
        'moved_registrations_count', v_moved_reg_count,
        'cancelled_registrations_count', v_cancelled_reg_count,
        'reason', COALESCE(NULLIF(BTRIM(p_reason), ''), 'Gộp hồ sơ thí sinh trùng lặp')
    );

    INSERT INTO audit_logs (
        audit_log_actor_account_id,
        audit_log_action,
        audit_log_entity_table,
        audit_log_entity_id,
        audit_log_old_value,
        audit_log_new_value,
        audit_log_created_at
    ) VALUES (
        p_actor_account_id,
        'MERGE',
        'candidates',
        p_primary_candidate_id,
        to_jsonb(v_dup_cand),
        v_summary,
        NOW() AT TIME ZONE 'UTC'
    );

    RETURN v_summary;
END;
$$ LANGUAGE plpgsql;

