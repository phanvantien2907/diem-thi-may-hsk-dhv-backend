-- =============================================================================
-- Rollback Migration 000019
-- =============================================================================

DROP FUNCTION IF EXISTS fn_merge_candidate_profiles(BIGINT, BIGINT, BIGINT, TEXT);

DROP INDEX IF EXISTS idx_candidate_docs_doc_number_active;
DROP INDEX IF EXISTS idx_candidates_review_lock;

ALTER TABLE candidates
    DROP COLUMN IF EXISTS candidate_last_viewed_at,
    DROP COLUMN IF EXISTS candidate_last_viewed_by,
    DROP COLUMN IF EXISTS candidate_review_locked_until,
    DROP COLUMN IF EXISTS candidate_review_locked_by;

UPDATE accounts
SET account_status = 'locked'
WHERE account_status = 'disabled';

ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_account_status_check;

ALTER TABLE accounts
    ADD CONSTRAINT accounts_account_status_check
    CHECK (account_status IN ('active', 'locked', 'pending'));

