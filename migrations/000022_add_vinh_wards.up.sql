-- 000022_add_vinh_wards.up.sql
-- Add missing administrative wards and communes for Nghệ An province (Thành phố Vinh)
-- to ensure 100% complete coverage for registration and OCR address auto-selection.

INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.wtype
FROM provinces p
CROSS JOIN (VALUES
    ('NA-P09', 'Phường Hà Huy Tập', 'phuong'),
    ('NA-P10', 'Phường Hưng Dũng',   'phuong'),
    ('NA-P11', 'Phường Cửa Nam',    'phuong'),
    ('NA-P12', 'Phường Hồng Sơn',   'phuong'),
    ('NA-P13', 'Phường Trung Đô',   'phuong'),
    ('NA-P14', 'Phường Vinh Tân',   'phuong'),
    ('NA-P15', 'Phường Đông Vĩnh',  'phuong'),
    ('NA-P16', 'Phường Quán Bàu',   'phuong'),
    ('NA-X06', 'Xã Hưng Chính',     'xa'),
    ('NA-X07', 'Xã Hưng Hòa',       'xa'),
    ('NA-X08', 'Xã Nghi Đức',       'xa'),
    ('NA-X09', 'Xã Nghi Liên',      'xa')
) AS w(code, name, wtype)
WHERE p.province_code = 'NA'
ON CONFLICT (ward_code) DO NOTHING;

