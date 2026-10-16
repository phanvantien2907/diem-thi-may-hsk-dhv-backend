-- =============================================================================
-- Migration 000013: Refund Lifecycle Closure & Suspense Statement Import
-- =============================================================================

-- 1. Cho phép trạng thái 'pending_refund' trong bảng payments
ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_payment_status_check;

ALTER TABLE payments
    ADD CONSTRAINT payments_payment_status_check
    CHECK (payment_status IN (
        'pending', 'success', 'failed', 'refunded', 'pending_refund',
        'expired', 'cancelled', 'amount_mismatch', 'partial_paid'
    ));

-- Thêm cột lưu bằng chứng hoàn tiền và thời điểm hoàn tiền xong
ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS payment_refund_evidence_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS payment_refunded_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_refund_bank_ref VARCHAR(100);

-- 2. Mở rộng suspense_source trong bảng payment_suspense_transactions
ALTER TABLE payment_suspense_transactions
    DROP CONSTRAINT IF EXISTS payment_suspense_transactions_suspense_source_check;

ALTER TABLE payment_suspense_transactions
    ADD CONSTRAINT payment_suspense_transactions_suspense_source_check
    CHECK (suspense_source IN ('webhook', 'manual', 'statement_import'));
