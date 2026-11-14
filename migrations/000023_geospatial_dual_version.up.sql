-- 000023_geospatial_dual_version.up.sql
-- Adds dual-version (v1 pre-merger 3-tier & v2 post-merger 2-tier) geospatial schema
-- supporting full Vietnam administrative boundaries and legacy CMND address resolution.

-- 1. Create districts table for legacy / pre-merger 3-tier administrative hierarchy
CREATE TABLE IF NOT EXISTS districts (
    district_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    district_province_id BIGINT       NOT NULL REFERENCES provinces (province_id),
    district_code        VARCHAR(20)  NOT NULL,
    district_name        VARCHAR(100) NOT NULL,
    district_type        VARCHAR(30)  NOT NULL DEFAULT 'huyen',
    district_version     VARCHAR(10)  NOT NULL DEFAULT 'v1',
    district_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    district_deleted_at  TIMESTAMPTZ,
    district_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    district_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_districts_code UNIQUE (district_code)
);

CREATE INDEX IF NOT EXISTS idx_districts_province
    ON districts (district_province_id) WHERE district_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_districts_name
    ON districts (district_name) WHERE district_is_deleted = FALSE;

CREATE OR REPLACE TRIGGER trg_districts_updated_at
    BEFORE UPDATE ON districts
    FOR EACH ROW EXECUTE FUNCTION fn_touch_updated_at('district_updated_at');

-- 2. Extend wards table to track v1 (legacy) vs v2 (current) version and merger target mapping
ALTER TABLE wards DROP CONSTRAINT IF EXISTS wards_ward_type_check;
ALTER TABLE wards ADD CONSTRAINT wards_ward_type_check
    CHECK (ward_type IN ('xa', 'phuong', 'dac_khu', 'thi_tran'));

ALTER TABLE wards
    ADD COLUMN IF NOT EXISTS ward_district_id   BIGINT REFERENCES districts (district_id),
    ADD COLUMN IF NOT EXISTS ward_district_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS ward_version       VARCHAR(10) NOT NULL DEFAULT 'v2',
    ADD COLUMN IF NOT EXISTS ward_legacy_name   VARCHAR(100),
    ADD COLUMN IF NOT EXISTS ward_target_v2_id  BIGINT REFERENCES wards (ward_id);

CREATE INDEX IF NOT EXISTS idx_wards_version
    ON wards (ward_version) WHERE ward_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_wards_district
    ON wards (ward_district_id) WHERE ward_district_id IS NOT NULL AND ward_is_deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_wards_legacy_name
    ON wards (ward_legacy_name) WHERE ward_legacy_name IS NOT NULL AND ward_is_deleted = FALSE;

