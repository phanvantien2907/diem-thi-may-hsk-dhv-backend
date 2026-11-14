-- 000023_geospatial_dual_version.down.sql

DROP INDEX IF EXISTS idx_wards_legacy_name;
DROP INDEX IF EXISTS idx_wards_district;
DROP INDEX IF EXISTS idx_wards_version;

ALTER TABLE wards
    DROP COLUMN IF EXISTS ward_target_v2_id,
    DROP COLUMN IF EXISTS ward_legacy_name,
    DROP COLUMN IF EXISTS ward_version,
    DROP COLUMN IF EXISTS ward_district_name,
    DROP COLUMN IF EXISTS ward_district_id;

ALTER TABLE wards DROP CONSTRAINT IF EXISTS wards_ward_type_check;
ALTER TABLE wards ADD CONSTRAINT wards_ward_type_check
    CHECK (ward_type IN ('xa', 'phuong', 'dac_khu'));

DROP TRIGGER IF EXISTS trg_districts_updated_at ON districts;
DROP INDEX IF EXISTS idx_districts_name;
DROP INDEX IF EXISTS idx_districts_province;
DROP TABLE IF EXISTS districts;

