-- =============================================================================
-- 001_create_tables.sql
-- HSK Vinh Registration System – Initial schema
-- Convention: every table uses prefixed columns (table_shortname_columnname)
--             and includes soft-delete via _deleted_at.
-- =============================================================================

-- ── Users ─────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    user_id         BIGSERIAL     PRIMARY KEY,
    user_email      VARCHAR(255)  NOT NULL UNIQUE,
    user_password   VARCHAR(255)  NOT NULL,
    user_full_name  VARCHAR(255)  NOT NULL,
    user_phone      VARCHAR(20),
    user_role       VARCHAR(20)   NOT NULL DEFAULT 'candidate'
                                  CHECK (user_role IN ('admin', 'staff', 'candidate')),
    user_created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    user_updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    user_deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email        ON users (user_email)      WHERE user_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_role         ON users (user_role)       WHERE user_deleted_at IS NULL;

-- ── Exam Sessions ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS exam_sessions (
    sess_id         BIGSERIAL     PRIMARY KEY,
    sess_name       VARCHAR(255)  NOT NULL,
    sess_level      SMALLINT      NOT NULL CHECK (sess_level BETWEEN 1 AND 6),
    sess_exam_date  DATE          NOT NULL,
    sess_location   VARCHAR(500)  NOT NULL,
    sess_max_slots  INT           NOT NULL CHECK (sess_max_slots > 0),
    sess_open_at    TIMESTAMPTZ   NOT NULL,
    sess_close_at   TIMESTAMPTZ   NOT NULL,
    sess_created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    sess_updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    sess_deleted_at TIMESTAMPTZ,
    CONSTRAINT chk_sess_dates CHECK (sess_close_at > sess_open_at)
);

CREATE INDEX IF NOT EXISTS idx_sess_exam_date     ON exam_sessions (sess_exam_date) WHERE sess_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sess_level         ON exam_sessions (sess_level)     WHERE sess_deleted_at IS NULL;

-- ── Exam Slots ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS exam_slots (
    slot_id         BIGSERIAL     PRIMARY KEY,
    slot_sess_id    BIGINT        NOT NULL REFERENCES exam_sessions (sess_id),
    slot_number     INT           NOT NULL CHECK (slot_number > 0),
    slot_status     VARCHAR(20)   NOT NULL DEFAULT 'open'
                                  CHECK (slot_status IN ('open', 'booked')),
    slot_created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    slot_updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    slot_deleted_at TIMESTAMPTZ,
    UNIQUE (slot_sess_id, slot_number)
);

CREATE INDEX IF NOT EXISTS idx_slots_sess_status  ON exam_slots (slot_sess_id, slot_status) WHERE slot_deleted_at IS NULL;

-- ── Registrations ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS registrations (
    reg_id          BIGSERIAL     PRIMARY KEY,
    reg_user_id     BIGINT        NOT NULL REFERENCES users (user_id),
    reg_sess_id     BIGINT        NOT NULL REFERENCES exam_sessions (sess_id),
    reg_slot_id     BIGINT        NOT NULL REFERENCES exam_slots (slot_id),
    reg_status      VARCHAR(20)   NOT NULL DEFAULT 'pending'
                                  CHECK (reg_status IN ('pending', 'confirmed', 'cancelled')),
    reg_notes       TEXT          NOT NULL DEFAULT '',
    reg_created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    reg_updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    reg_deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reg_user_id        ON registrations (reg_user_id)  WHERE reg_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_reg_sess_id        ON registrations (reg_sess_id)  WHERE reg_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_reg_status         ON registrations (reg_status)   WHERE reg_deleted_at IS NULL;

-- ── Payments ──────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS payments (
    pay_id          BIGSERIAL     PRIMARY KEY,
    pay_reg_id      BIGINT        NOT NULL REFERENCES registrations (reg_id),
    pay_amount      NUMERIC(12,2) NOT NULL CHECK (pay_amount >= 0),
    pay_currency    VARCHAR(3)    NOT NULL DEFAULT 'VND',
    pay_status      VARCHAR(20)   NOT NULL DEFAULT 'pending'
                                  CHECK (pay_status IN ('pending', 'paid', 'failed', 'refunded')),
    pay_method      VARCHAR(50)   NOT NULL,
    pay_reference   VARCHAR(255)  NOT NULL DEFAULT '',
    pay_paid_at     TIMESTAMPTZ,
    pay_created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    pay_updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    pay_deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_pay_reg_id         ON payments (pay_reg_id)  WHERE pay_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_pay_status         ON payments (pay_status)  WHERE pay_deleted_at IS NULL;
