DROP TABLE IF EXISTS payment_suspense_transactions CASCADE;

ALTER TABLE payments
    DROP COLUMN IF EXISTS payment_evidence_urls,
    DROP COLUMN IF EXISTS payment_admin_note,
    DROP COLUMN IF EXISTS payment_refund_amount,
    DROP COLUMN IF EXISTS payment_refund_reason,
    DROP COLUMN IF EXISTS payment_is_top_up,
    DROP COLUMN IF EXISTS payment_parent_payment_id;
