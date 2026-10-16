-- =============================================================================
-- Migration 000013: Revert Refund Lifecycle Closure & Suspense Statement Import
-- =============================================================================

ALTER TABLE payment_suspense_transactions
    DROP CONSTRAINT IF EXISTS payment_suspense_transactions_suspense_source_check;

ALTER TABLE payment_suspense_transactions
    ADD CONSTRAINT payment_suspense_transactions_suspense_source_check
    CHECK (suspense_source IN ('webhook', 'manual'));

ALTER TABLE payments
    DROP COLUMN IF EXISTS payment_refund_bank_ref,
    DROP COLUMN IF EXISTS payment_refunded_at,
    DROP COLUMN IF EXISTS payment_refund_evidence_urls;

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_payment_status_check;

ALTER TABLE payments
    ADD CONSTRAINT payments_payment_status_check
    CHECK (payment_status IN (
        'pending', 'success', 'failed', 'refunded',
        'expired', 'cancelled', 'amount_mismatch', 'partial_paid'
    ));
