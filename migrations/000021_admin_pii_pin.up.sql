-- 000021_admin_pii_pin.up.sql
-- Bổ sung Cổng Bảo Mật Mã PIN 6 Số (Zero-Trust PII Gateway) cho tài khoản Quản trị viên (Nghị định 13/2023/NĐ-CP)
-- Lưu trữ mã băm bcrypt (tuyệt đối không lưu plain text) và cơ chế chống dò mã PIN (khóa tạm thời sau 5 lần nhập sai).

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS account_pii_pin_hash         VARCHAR(255) NULL,
    ADD COLUMN IF NOT EXISTS account_pii_pin_failed_count INT          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS account_pii_pin_locked_until TIMESTAMPTZ  NULL,
    ADD COLUMN IF NOT EXISTS account_pii_pin_updated_at   TIMESTAMPTZ  NULL;

