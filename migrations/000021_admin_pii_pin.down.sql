-- 000021_admin_pii_pin.down.sql

ALTER TABLE accounts
    DROP COLUMN IF EXISTS account_pii_pin_updated_at,
    DROP COLUMN IF EXISTS account_pii_pin_locked_until,
    DROP COLUMN IF EXISTS account_pii_pin_failed_count,
    DROP COLUMN IF EXISTS account_pii_pin_hash;

