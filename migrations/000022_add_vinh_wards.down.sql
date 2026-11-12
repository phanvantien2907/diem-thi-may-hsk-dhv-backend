-- 000022_add_vinh_wards.down.sql
DELETE FROM wards
WHERE ward_code IN (
    'NA-P09', 'NA-P10', 'NA-P11', 'NA-P12', 'NA-P13', 'NA-P14', 'NA-P15', 'NA-P16',
    'NA-X06', 'NA-X07', 'NA-X08', 'NA-X09'
);

