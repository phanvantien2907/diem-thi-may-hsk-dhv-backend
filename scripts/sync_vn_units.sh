#!/bin/bash
# =====================================================================
# SYNC DỮ LIỆU TỈNH/XÃ TỪ NGUỒN THẬT (thanglequoc/vietnamese-provinces-database)
# Đã điều chỉnh để chạy hoàn hảo với Docker container hiện tại.
# =====================================================================
set -e  # Dừng ngay nếu có lỗi ở bất kỳ bước nào

DB="hsk_vinh"
DB_USER="postgres"
CONTAINER_NAME="hsk_vinh_postgres"

echo ">> Bước 1: Tải 2 file SQL mới nhất từ GitHub"
curl -sL "https://raw.githubusercontent.com/thanglequoc/vietnamese-provinces-database/master/postgresql/postgres_CreateTables_vn_units.sql" -o /tmp/vn_schema.sql
curl -sL "https://raw.githubusercontent.com/thanglequoc/vietnamese-provinces-database/master/postgresql/postgres_ImportData_vn_units.sql" -o /tmp/vn_data.sql

echo ">> Bước 2: Copy 2 file SQL vào trong container"
docker cp /tmp/vn_schema.sql $CONTAINER_NAME:/tmp/vn_schema.sql
docker cp /tmp/vn_data.sql $CONTAINER_NAME:/tmp/vn_data.sql

echo ">> Bước 3: Tạo schema tạm 'vn_source' (xoá nếu đã tồn tại)"
docker exec -i $CONTAINER_NAME psql -U $DB_USER -d $DB -c "DROP SCHEMA IF EXISTS vn_source CASCADE;"
docker exec -i $CONTAINER_NAME psql -U $DB_USER -d $DB -c "CREATE SCHEMA vn_source;"

echo ">> Bước 4: Nạp schema + data gốc vào vn_source (KHÔNG sửa file gốc)"
# Thay vì dùng -c với \i (gây lỗi cú pháp), ta truyền trực tiếp PGOPTIONS vào biến môi trường của container và dùng -f
docker exec -e PGOPTIONS='-c search_path=vn_source,public' -i $CONTAINER_NAME psql -U $DB_USER -d $DB -v ON_ERROR_STOP=1 -f /tmp/vn_schema.sql
docker exec -e PGOPTIONS='-c search_path=vn_source,public' -i $CONTAINER_NAME psql -U $DB_USER -d $DB -v ON_ERROR_STOP=1 -f /tmp/vn_data.sql

echo ">> Bước 5: Copy sang bảng chính (UPSERT - bọc trong transaction để an toàn tuyệt đối)"
docker exec -i $CONTAINER_NAME psql -U $DB_USER -d $DB -v ON_ERROR_STOP=1 <<'SQL'
BEGIN;

-- 1. Upsert Provinces
-- Bảng provinces của bạn có UNIQUE(province_code)
INSERT INTO provinces (province_code, province_name)
SELECT code, full_name 
FROM vn_source.provinces
ON CONFLICT (province_code) 
DO UPDATE SET 
    province_name = EXCLUDED.province_name,
    province_updated_at = NOW();

-- 2. Upsert Wards
-- Bảng wards của bạn có UNIQUE(ward_code), KHÔNG PHẢI (ward_province_id, ward_code)
INSERT INTO wards (ward_province_id, ward_code, ward_name)
SELECT p.province_id, w.code, w.full_name
FROM vn_source.wards w
JOIN provinces p ON p.province_code = w.province_code
ON CONFLICT (ward_code) 
DO UPDATE SET 
    ward_name = EXCLUDED.ward_name,
    ward_province_id = EXCLUDED.ward_province_id,
    ward_updated_at = NOW();

COMMIT;
SQL

echo ">> Bước 6: Dọn schema tạm và file rác"
docker exec -i $CONTAINER_NAME psql -U $DB_USER -d $DB -c "DROP SCHEMA vn_source CASCADE;"
docker exec -i $CONTAINER_NAME rm /tmp/vn_schema.sql /tmp/vn_data.sql
rm -f /tmp/vn_schema.sql /tmp/vn_data.sql

echo ">> XONG. Kiểm tra số lượng sau khi đồng bộ:"
docker exec -i $CONTAINER_NAME psql -U $DB_USER -d $DB -c "SELECT count(*) AS so_tinh FROM provinces; SELECT count(*) AS so_xa FROM wards;"
