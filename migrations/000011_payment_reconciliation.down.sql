DROP INDEX IF EXISTS idx_payments_reconciliation;
ALTER TABLE payments DROP COLUMN IF EXISTS payment_amount_received;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_payment_status_check;
ALTER TABLE payments
    ADD CONSTRAINT payments_payment_status_check
    CHECK (payment_status IN ('pending', 'success', 'failed', 'refunded', 'expired'));
