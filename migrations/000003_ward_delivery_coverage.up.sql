-- =============================================================================
-- 003_ward_delivery_coverage.sql
-- Add delivery coverage flags to wards for courier pre-check feature
-- =============================================================================

ALTER TABLE wards
    ADD COLUMN IF NOT EXISTS ward_ghn_coverage  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS ward_ghtk_coverage BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN wards.ward_ghn_coverage  IS 'TRUE = GHN delivers to this ward';
COMMENT ON COLUMN wards.ward_ghtk_coverage IS 'TRUE = GHTK delivers to this ward';
