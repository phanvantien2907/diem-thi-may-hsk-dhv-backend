-- =============================================================================
-- 001_schema.sql
-- HSK Vinh Registration System – Complete database schema
-- PostgreSQL 16 | Soft-delete | Prefixed columns | BIGINT IDENTITY PKs
-- All 19 tables matching Go models 1:1
-- Run order: this file first, then 002_seed_data.sql
-- =============================================================================

-- ── Extensions ───────────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- digest() for verification codes

-- ── Generic auto-updated_at trigger ─────────────────────────────────────────
-- Single function reused by all 19 tables via TG_ARGV[0] = column name.
CREATE OR REPLACE FUNCTION fn_touch_updated_at() RETURNS trigger AS $$
DECLARE
    v_col  text := TG_ARGV[0];
    v_data jsonb;
BEGIN
    v_data := to_jsonb(NEW) || jsonb_build_object(v_col, now());
    NEW := jsonb_populate_record(NEW, v_data);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- 1. REFERENCE TABLES
-- =============================================================================

-- 1.1 roles
CREATE TABLE IF NOT EXISTS roles (
    role_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    role_code       VARCHAR(30)  NOT NULL,
    role_name       VARCHAR(100) NOT NULL,
    role_is_deleted BOOLEAN      NOT NULL DEFAULT FALSE,
    role_deleted_at TIMESTAMPTZ,
    role_created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    role_updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_roles_code UNIQUE (role_code)
);
CREATE INDEX IF NOT EXISTS idx_roles_code ON roles (role_code) WHERE role_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_roles_updated_at
    BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('role_updated_at');

-- 1.2 exam_types
CREATE TABLE IF NOT EXISTS exam_types (
    exam_type_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_type_code        VARCHAR(20)  NOT NULL,
    exam_type_name        VARCHAR(100) NOT NULL,
    exam_type_description TEXT,
    exam_type_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    exam_type_deleted_at  TIMESTAMPTZ,
    exam_type_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    exam_type_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_exam_types_code UNIQUE (exam_type_code)
);
CREATE INDEX IF NOT EXISTS idx_exam_types_code ON exam_types (exam_type_code) WHERE exam_type_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_types_updated_at
    BEFORE UPDATE ON exam_types
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_type_updated_at');

-- 1.3 provinces
-- NOTE: From 01/07/2025 Vietnam uses 2-level admin structure (province → ward).
-- No districts table.
CREATE TABLE IF NOT EXISTS provinces (
    province_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    province_code       VARCHAR(20)  NOT NULL,
    province_name       VARCHAR(100) NOT NULL,
    province_is_deleted BOOLEAN      NOT NULL DEFAULT FALSE,
    province_deleted_at TIMESTAMPTZ,
    province_created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    province_updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_provinces_code UNIQUE (province_code)
);
CREATE INDEX IF NOT EXISTS idx_provinces_name ON provinces (province_name) WHERE province_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_provinces_updated_at
    BEFORE UPDATE ON provinces
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('province_updated_at');

-- 1.4 wards
CREATE TABLE IF NOT EXISTS wards (
    ward_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ward_province_id BIGINT       NOT NULL REFERENCES provinces (province_id),
    ward_code        VARCHAR(20)  NOT NULL,
    ward_name        VARCHAR(100) NOT NULL,
    ward_type        VARCHAR(10)  NOT NULL DEFAULT 'xa'
                          CHECK (ward_type IN ('xa', 'phuong', 'dac_khu')),
    ward_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    ward_deleted_at  TIMESTAMPTZ,
    ward_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ward_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_wards_code UNIQUE (ward_code)
);
CREATE INDEX IF NOT EXISTS idx_wards_province ON wards (ward_province_id) WHERE ward_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_wards_name     ON wards (ward_name)        WHERE ward_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_wards_updated_at
    BEFORE UPDATE ON wards
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('ward_updated_at');

-- =============================================================================
-- 2. IDENTITY & CANDIDATE PROFILE
-- =============================================================================

-- 2.1 accounts (used by candidates, admins, proctors, reviewers)
CREATE TABLE IF NOT EXISTS accounts (
    account_id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_role_id       BIGINT       NOT NULL REFERENCES roles (role_id),
    account_username      VARCHAR(50)  NOT NULL,
    account_password_hash VARCHAR(255) NOT NULL,
    account_email         VARCHAR(150) NOT NULL,
    account_phone         VARCHAR(20),
    account_status        VARCHAR(20)  NOT NULL DEFAULT 'active'
                               CHECK (account_status IN ('active', 'locked', 'pending')),
    account_last_login_at TIMESTAMPTZ,
    account_is_deleted    BOOLEAN      NOT NULL DEFAULT FALSE,
    account_deleted_at    TIMESTAMPTZ,
    account_created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    account_updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_accounts_username UNIQUE (account_username),
    CONSTRAINT uq_accounts_email    UNIQUE (account_email)
);
CREATE INDEX IF NOT EXISTS idx_accounts_username ON accounts (account_username) WHERE account_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_accounts_email    ON accounts (account_email)    WHERE account_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_accounts_role     ON accounts (account_role_id)  WHERE account_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_accounts_updated_at
    BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('account_updated_at');

-- 2.2 candidates (1-1 with accounts for candidate-role accounts)
CREATE TABLE IF NOT EXISTS candidates (
    candidate_id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    candidate_account_id          BIGINT       NOT NULL REFERENCES accounts (account_id),
    candidate_full_name           VARCHAR(150) NOT NULL,
    candidate_full_name_cn        VARCHAR(150),
    candidate_dob                 DATE         NOT NULL,
    candidate_gender              VARCHAR(10)  NOT NULL
                                       CHECK (candidate_gender IN ('male', 'female', 'other')),
    candidate_ethnicity           VARCHAR(50),
    candidate_religion            VARCHAR(50),
    candidate_birthplace          VARCHAR(150),
    candidate_nationality         VARCHAR(50)  NOT NULL DEFAULT 'Việt Nam',
    candidate_mother_tongue       VARCHAR(50),
    candidate_chinese_study_years SMALLINT     CHECK (candidate_chinese_study_years >= 0),
    candidate_is_deleted          BOOLEAN      NOT NULL DEFAULT FALSE,
    candidate_deleted_at          TIMESTAMPTZ,
    candidate_created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    candidate_updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_candidates_account UNIQUE (candidate_account_id)
);
CREATE INDEX IF NOT EXISTS idx_candidates_account  ON candidates (candidate_account_id) WHERE candidate_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_candidates_fullname ON candidates (candidate_full_name)  WHERE candidate_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_candidates_updated_at
    BEFORE UPDATE ON candidates
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('candidate_updated_at');

-- 2.3 candidate_documents (CCCD/passport + photo + address)
CREATE TABLE IF NOT EXISTS candidate_documents (
    candidate_document_id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    candidate_document_candidate_id        BIGINT       NOT NULL REFERENCES candidates (candidate_id),
    candidate_document_type                VARCHAR(20)  NOT NULL
                                                CHECK (candidate_document_type IN ('cccd', 'passport')),
    candidate_document_number              VARCHAR(30)  NOT NULL,
    candidate_document_issue_date          DATE         NOT NULL,
    candidate_document_issue_place         VARCHAR(150) NOT NULL,
    candidate_document_front_image_url     VARCHAR(255) NOT NULL,
    candidate_document_back_image_url      VARCHAR(255),
    candidate_document_portrait_image_url  VARCHAR(255) NOT NULL,
    candidate_document_verification_status VARCHAR(20)  NOT NULL DEFAULT 'pending'
                                                CHECK (candidate_document_verification_status IN ('pending', 'verified', 'rejected')),
    candidate_document_ward_id             BIGINT       REFERENCES wards (ward_id),
    candidate_document_address_detail      VARCHAR(255),
    candidate_document_is_deleted          BOOLEAN      NOT NULL DEFAULT FALSE,
    candidate_document_deleted_at          TIMESTAMPTZ,
    candidate_document_created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    candidate_document_updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_candidate_docs_candidate
    ON candidate_documents (candidate_document_candidate_id) WHERE candidate_document_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_candidate_docs_status
    ON candidate_documents (candidate_document_verification_status) WHERE candidate_document_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_candidate_documents_updated_at
    BEFORE UPDATE ON candidate_documents
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('candidate_document_updated_at');

-- =============================================================================
-- 3. EXAM OPERATIONS
-- =============================================================================

-- 3.1 exam_rooms
CREATE TABLE IF NOT EXISTS exam_rooms (
    exam_room_id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_room_name      VARCHAR(100) NOT NULL,
    exam_room_location  VARCHAR(255),
    exam_room_capacity  INT          NOT NULL CHECK (exam_room_capacity > 0),
    exam_room_is_deleted BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_room_deleted_at TIMESTAMPTZ,
    exam_room_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_room_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE OR REPLACE TRIGGER trg_exam_rooms_updated_at
    BEFORE UPDATE ON exam_rooms
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_room_updated_at');

-- 3.2 exam_sessions
CREATE TABLE IF NOT EXISTS exam_sessions (
    exam_session_id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_session_exam_type_id         BIGINT        NOT NULL REFERENCES exam_types (exam_type_id),
    exam_session_exam_room_id         BIGINT        NOT NULL REFERENCES exam_rooms (exam_room_id),
    exam_session_date                 DATE          NOT NULL,
    exam_session_shift                VARCHAR(10)   NOT NULL
                                           CHECK (exam_session_shift IN ('morning', 'afternoon', 'evening')),
    exam_session_capacity             INT           NOT NULL CHECK (exam_session_capacity > 0),
    exam_session_fee                  NUMERIC(12,0) NOT NULL CHECK (exam_session_fee >= 0),
    exam_session_registration_deadline TIMESTAMPTZ  NOT NULL,
    exam_session_status               VARCHAR(20)   NOT NULL DEFAULT 'open'
                                           CHECK (exam_session_status IN ('open', 'closed', 'cancelled')),
    exam_session_is_deleted           BOOLEAN       NOT NULL DEFAULT FALSE,
    exam_session_deleted_at           TIMESTAMPTZ,
    exam_session_created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    exam_session_updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_exam_sessions_date   ON exam_sessions (exam_session_date)   WHERE exam_session_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_exam_sessions_status ON exam_sessions (exam_session_status) WHERE exam_session_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_sessions_updated_at
    BEFORE UPDATE ON exam_sessions
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_session_updated_at');

-- 3.3 exam_seats (one row per physical seat; SELECT FOR UPDATE SKIP LOCKED for concurrency)
CREATE TABLE IF NOT EXISTS exam_seats (
    exam_seat_id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_seat_exam_session_id BIGINT     NOT NULL REFERENCES exam_sessions (exam_session_id),
    exam_seat_number         SMALLINT    NOT NULL,
    exam_seat_status         VARCHAR(20) NOT NULL DEFAULT 'available'
                                  CHECK (exam_seat_status IN ('available', 'held', 'booked')),
    exam_seat_held_until     TIMESTAMPTZ,
    exam_seat_is_deleted     BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_seat_deleted_at     TIMESTAMPTZ,
    exam_seat_created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_seat_updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_exam_seats_session_number UNIQUE (exam_seat_exam_session_id, exam_seat_number)
);
CREATE INDEX IF NOT EXISTS idx_exam_seats_available
    ON exam_seats (exam_seat_exam_session_id, exam_seat_status)
    WHERE exam_seat_status = 'available' AND exam_seat_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_seats_updated_at
    BEFORE UPDATE ON exam_seats
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_seat_updated_at');

-- 3.4 exam_registrations
CREATE TABLE IF NOT EXISTS exam_registrations (
    exam_registration_id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_registration_candidate_id    BIGINT      NOT NULL REFERENCES candidates (candidate_id),
    exam_registration_exam_session_id BIGINT      NOT NULL REFERENCES exam_sessions (exam_session_id),
    exam_registration_exam_seat_id    BIGINT      NOT NULL REFERENCES exam_seats (exam_seat_id),
    exam_registration_status          VARCHAR(20) NOT NULL DEFAULT 'pending_payment'
                                           CHECK (exam_registration_status IN ('pending_payment', 'confirmed', 'cancelled')),
    exam_registration_registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_registration_is_deleted      BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_registration_deleted_at      TIMESTAMPTZ,
    exam_registration_created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_registration_updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_exam_reg_candidate
    ON exam_registrations (exam_registration_candidate_id) WHERE exam_registration_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_exam_reg_session
    ON exam_registrations (exam_registration_exam_session_id) WHERE exam_registration_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_exam_reg_status
    ON exam_registrations (exam_registration_status) WHERE exam_registration_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_registrations_updated_at
    BEFORE UPDATE ON exam_registrations
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_registration_updated_at');

-- =============================================================================
-- 4. PAYMENTS, RESULTS, RECHECKS
-- =============================================================================

-- 4.1 payments
CREATE TABLE IF NOT EXISTS payments (
    payment_id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_exam_registration_id BIGINT        NOT NULL REFERENCES exam_registrations (exam_registration_id),
    payment_amount               NUMERIC(12,0) NOT NULL CHECK (payment_amount >= 0),
    payment_method               VARCHAR(20)   NOT NULL
                                      CHECK (payment_method IN ('vnpay', 'momo', 'zalopay', 'bank_transfer', 'cash')),
    payment_transaction_ref      VARCHAR(100),
    payment_status               VARCHAR(20)   NOT NULL DEFAULT 'pending'
                                      CHECK (payment_status IN ('pending', 'success', 'failed', 'refunded')),
    payment_paid_at              TIMESTAMPTZ,
    payment_is_deleted           BOOLEAN       NOT NULL DEFAULT FALSE,
    payment_deleted_at           TIMESTAMPTZ,
    payment_created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    payment_updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_payments_registration
    ON payments (payment_exam_registration_id) WHERE payment_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_payments_transaction_ref
    ON payments (payment_transaction_ref) WHERE payment_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_payments_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('payment_updated_at');

-- 4.2 exam_results (1-1 with exam_registrations)
CREATE TABLE IF NOT EXISTS exam_results (
    exam_result_id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_result_exam_registration_id BIGINT       NOT NULL REFERENCES exam_registrations (exam_registration_id),
    exam_result_listening_score      NUMERIC(5,1) CHECK (exam_result_listening_score BETWEEN 0 AND 100),
    exam_result_reading_score        NUMERIC(5,1) CHECK (exam_result_reading_score  BETWEEN 0 AND 100),
    exam_result_writing_score        NUMERIC(5,1) CHECK (exam_result_writing_score  BETWEEN 0 AND 100),
    exam_result_total_score          NUMERIC(6,1),
    exam_result_status               VARCHAR(10)  CHECK (exam_result_status IN ('pass', 'fail')),
    exam_result_published_at         TIMESTAMPTZ,
    exam_result_is_deleted           BOOLEAN      NOT NULL DEFAULT FALSE,
    exam_result_deleted_at           TIMESTAMPTZ,
    exam_result_created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    exam_result_updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_exam_results_registration UNIQUE (exam_result_exam_registration_id)
);
CREATE INDEX IF NOT EXISTS idx_exam_results_status
    ON exam_results (exam_result_status) WHERE exam_result_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_results_updated_at
    BEFORE UPDATE ON exam_results
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_result_updated_at');

-- 4.3 exam_recheck_requests
CREATE TABLE IF NOT EXISTS exam_recheck_requests (
    exam_recheck_id                     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_recheck_exam_result_id         BIGINT      NOT NULL REFERENCES exam_results (exam_result_id),
    exam_recheck_reason                 TEXT        NOT NULL,
    exam_recheck_status                 VARCHAR(20) NOT NULL DEFAULT 'submitted'
                                             CHECK (exam_recheck_status IN ('submitted', 'in_review', 'completed', 'rejected')),
    exam_recheck_reviewed_by_account_id BIGINT      REFERENCES accounts (account_id),
    exam_recheck_reviewed_at            TIMESTAMPTZ,
    exam_recheck_new_total_score        NUMERIC(6,1),
    exam_recheck_is_deleted             BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_recheck_deleted_at             TIMESTAMPTZ,
    exam_recheck_created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_recheck_updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_exam_recheck_result
    ON exam_recheck_requests (exam_recheck_exam_result_id) WHERE exam_recheck_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_exam_recheck_updated_at
    BEFORE UPDATE ON exam_recheck_requests
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_recheck_updated_at');

-- =============================================================================
-- 5. CERTIFICATES
-- =============================================================================

-- 5.1 certificates (1-1 with exam_results for passing candidates)
CREATE TABLE IF NOT EXISTS certificates (
    certificate_id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    certificate_exam_result_id   BIGINT      NOT NULL REFERENCES exam_results (exam_result_id),
    certificate_no               VARCHAR(30) NOT NULL,
    certificate_issue_date       DATE        NOT NULL,
    certificate_verification_code VARCHAR(64) NOT NULL,
    certificate_status           VARCHAR(20) NOT NULL DEFAULT 'issued'
                                      CHECK (certificate_status IN ('issued', 'revoked')),
    certificate_is_deleted       BOOLEAN     NOT NULL DEFAULT FALSE,
    certificate_deleted_at       TIMESTAMPTZ,
    certificate_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    certificate_updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_certificates_result UNIQUE (certificate_exam_result_id),
    CONSTRAINT uq_certificates_no     UNIQUE (certificate_no),
    CONSTRAINT uq_certificates_code   UNIQUE (certificate_verification_code)
);
CREATE OR REPLACE TRIGGER trg_certificates_updated_at
    BEFORE UPDATE ON certificates
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('certificate_updated_at');

-- 5.2 certificate_deliveries
CREATE TABLE IF NOT EXISTS certificate_deliveries (
    certificate_delivery_id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    certificate_delivery_certificate_id BIGINT      NOT NULL REFERENCES certificates (certificate_id),
    certificate_delivery_ward_id        BIGINT      REFERENCES wards (ward_id),
    certificate_delivery_address_detail VARCHAR(255) NOT NULL,
    certificate_delivery_courier        VARCHAR(50),
    certificate_delivery_tracking_no    VARCHAR(50),
    certificate_delivery_status         VARCHAR(20) NOT NULL DEFAULT 'requested'
                                             CHECK (certificate_delivery_status IN ('requested', 'shipped', 'delivered', 'failed')),
    certificate_delivery_is_deleted     BOOLEAN     NOT NULL DEFAULT FALSE,
    certificate_delivery_deleted_at     TIMESTAMPTZ,
    certificate_delivery_created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    certificate_delivery_updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cert_deliveries_certificate
    ON certificate_deliveries (certificate_delivery_certificate_id) WHERE certificate_delivery_is_deleted = FALSE;
CREATE OR REPLACE TRIGGER trg_certificate_deliveries_updated_at
    BEFORE UPDATE ON certificate_deliveries
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('certificate_delivery_updated_at');
CREATE UNIQUE INDEX ux_certificates_result
    ON certificates (certificate_exam_result_id)
    WHERE NOT certificate_is_deleted;

-- =============================================================================
-- 6. PARTITIONED TIME-SERIES TABLES
-- =============================================================================

-- 6.1 notifications — partitioned by month (notification_created_at)
CREATE TABLE IF NOT EXISTS notifications (
    notification_id         BIGINT GENERATED ALWAYS AS IDENTITY,
    notification_account_id BIGINT      NOT NULL REFERENCES accounts (account_id),
    notification_channel    VARCHAR(10) NOT NULL
                                 CHECK (notification_channel IN ('email', 'sms', 'zalo', 'push')),
    notification_content    TEXT        NOT NULL,
    notification_status     VARCHAR(10) NOT NULL DEFAULT 'pending'
                                 CHECK (notification_status IN ('pending', 'sent', 'failed')),
    notification_is_deleted BOOLEAN     NOT NULL DEFAULT FALSE,
    notification_deleted_at TIMESTAMPTZ,
    notification_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    notification_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (notification_id, notification_created_at)
) PARTITION BY RANGE (notification_created_at);

-- Default partition catches anything outside explicit monthly partitions
CREATE TABLE IF NOT EXISTS notifications_default
    PARTITION OF notifications DEFAULT;

-- Create partitions for the next 6 months from Aug 2026
CREATE TABLE IF NOT EXISTS notifications_2026_08
    PARTITION OF notifications FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE IF NOT EXISTS notifications_2026_09
    PARTITION OF notifications FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE IF NOT EXISTS notifications_2026_10
    PARTITION OF notifications FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE IF NOT EXISTS notifications_2026_11
    PARTITION OF notifications FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE IF NOT EXISTS notifications_2026_12
    PARTITION OF notifications FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');
CREATE TABLE IF NOT EXISTS notifications_2027_01
    PARTITION OF notifications FOR VALUES FROM ('2027-01-01') TO ('2027-02-01');

-- 6.2 audit_logs — partitioned by month; IMMUTABLE (no soft-delete by design)
CREATE TABLE IF NOT EXISTS audit_logs (
    audit_log_id               BIGINT GENERATED ALWAYS AS IDENTITY,
    audit_log_actor_account_id BIGINT      REFERENCES accounts (account_id),
    audit_log_action           VARCHAR(50) NOT NULL,
    audit_log_entity_table     VARCHAR(50) NOT NULL,
    audit_log_entity_id        BIGINT      NOT NULL,
    audit_log_old_value        JSONB,
    audit_log_new_value        JSONB,
    audit_log_created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (audit_log_id, audit_log_created_at)
) PARTITION BY RANGE (audit_log_created_at);

-- 7.1 auth_sessions (replacement for revoked tokens)
CREATE TABLE auth_sessions (
    auth_session_id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    auth_session_account_id     BIGINT NOT NULL REFERENCES accounts (account_id),
    auth_session_refresh_token_hash VARCHAR(255) NOT NULL,
    auth_session_device_info    VARCHAR(255),
    auth_session_ip_address     INET,
    auth_session_expires_at     TIMESTAMPTZ NOT NULL,
    auth_session_revoked_at     TIMESTAMPTZ,
    auth_session_created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 7.2 auth_otp_codes (replacement for one_time_passwords)
CREATE TABLE auth_otp_codes (
    auth_otp_id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    auth_otp_account_id    BIGINT NOT NULL REFERENCES accounts (account_id),
    auth_otp_code_hash     VARCHAR(255) NOT NULL,
    auth_otp_purpose       VARCHAR(30) NOT NULL CHECK (auth_otp_purpose IN ('register_verify','reset_password')),
    auth_otp_attempt_count SMALLINT NOT NULL DEFAULT 0,
    auth_otp_expires_at    TIMESTAMPTZ NOT NULL,
    auth_otp_consumed_at   TIMESTAMPTZ,
    auth_otp_created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Default partition
CREATE TABLE IF NOT EXISTS audit_logs_default
    PARTITION OF audit_logs DEFAULT;

CREATE TABLE IF NOT EXISTS audit_logs_2026_08
    PARTITION OF audit_logs FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_09
    PARTITION OF audit_logs FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_10
    PARTITION OF audit_logs FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_11
    PARTITION OF audit_logs FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_12
    PARTITION OF audit_logs FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');
CREATE TABLE IF NOT EXISTS audit_logs_2027_01
    PARTITION OF audit_logs FOR VALUES FROM ('2027-01-01') TO ('2027-02-01');
