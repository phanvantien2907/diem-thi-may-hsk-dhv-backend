-- 000014_exam_periods_and_management.up.sql
-- Management of Exam Periods (Đợt thi) and Exam Sessions (Ca thi)

CREATE TABLE IF NOT EXISTS exam_periods (
    exam_period_id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_period_name                  VARCHAR(150) NOT NULL,
    exam_period_code                  VARCHAR(50)  NOT NULL,
    exam_period_description           TEXT,
    exam_period_instructions          TEXT,
    exam_period_start_date            DATE,
    exam_period_end_date              DATE,
    exam_period_registration_start    TIMESTAMPTZ,
    exam_period_registration_deadline TIMESTAMPTZ,
    exam_period_status                VARCHAR(30)  NOT NULL DEFAULT 'draft'
                                           CHECK (exam_period_status IN ('draft', 'published', 'registration_open', 'registration_closed', 'rescheduled', 'cancelled', 'completed')),
    exam_period_is_deleted            BOOLEAN      NOT NULL DEFAULT FALSE,
    exam_period_deleted_at            TIMESTAMPTZ,
    exam_period_created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    exam_period_updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_exam_periods_code UNIQUE (exam_period_code)
);

CREATE INDEX IF NOT EXISTS idx_exam_periods_code
    ON exam_periods (exam_period_code) WHERE exam_period_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_exam_periods_status
    ON exam_periods (exam_period_status) WHERE exam_period_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_exam_periods_dates
    ON exam_periods (exam_period_start_date, exam_period_end_date) WHERE exam_period_is_deleted = FALSE;

CREATE OR REPLACE TRIGGER trg_exam_periods_updated_at
    BEFORE UPDATE ON exam_periods
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('exam_period_updated_at');

-- Link exam_sessions to exam_periods and allow NULL dates for duplicated draft sessions
ALTER TABLE exam_sessions
    ADD COLUMN IF NOT EXISTS exam_session_exam_period_id BIGINT REFERENCES exam_periods (exam_period_id),
    ALTER COLUMN exam_session_date DROP NOT NULL,
    ALTER COLUMN exam_session_registration_deadline DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_exam_sessions_period
    ON exam_sessions (exam_session_exam_period_id) WHERE exam_session_is_deleted = FALSE;

-- Populate initial exam_periods from existing sessions grouped by distinct exam_session_date
INSERT INTO exam_periods (
    exam_period_name,
    exam_period_code,
    exam_period_start_date,
    exam_period_end_date,
    exam_period_registration_deadline,
    exam_period_status
)
SELECT
    'Đợt thi HSK ngày ' || TO_CHAR(exam_session_date, 'DD/MM/YYYY') AS name,
    'HSK-' || TO_CHAR(exam_session_date, 'YYYY-MM-DD') AS code,
    exam_session_date,
    exam_session_date,
    MAX(exam_session_registration_deadline),
    'published'
FROM exam_sessions
WHERE exam_session_is_deleted = FALSE AND exam_session_date IS NOT NULL
GROUP BY exam_session_date
ON CONFLICT (exam_period_code) DO NOTHING;

-- Link existing sessions to their corresponding created periods
UPDATE exam_sessions es
SET exam_session_exam_period_id = ep.exam_period_id
FROM exam_periods ep
WHERE es.exam_session_exam_period_id IS NULL
  AND es.exam_session_date IS NOT NULL
  AND ep.exam_period_code = 'HSK-' || TO_CHAR(es.exam_session_date, 'YYYY-MM-DD');
