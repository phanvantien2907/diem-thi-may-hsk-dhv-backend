-- =============================================================================
-- 002_seed_data.sql
-- HSK Vinh Registration System – Realistic Vietnamese seed data
-- Run AFTER 001_schema.sql (or 01_schema.sql)
-- =============================================================================

-- ── 1. Partitions (must exist before inserting partitioned tables) ─────────
-- notifications partitions
CREATE TABLE IF NOT EXISTS notifications_2026_08
    PARTITION OF notifications
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE TABLE IF NOT EXISTS notifications_2026_09
    PARTITION OF notifications
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

CREATE TABLE IF NOT EXISTS notifications_2026_10
    PARTITION OF notifications
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');

-- audit_logs partitions
CREATE TABLE IF NOT EXISTS audit_logs_2026_08
    PARTITION OF audit_logs
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE TABLE IF NOT EXISTS audit_logs_2026_09
    PARTITION OF audit_logs
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

-- ── 2. Roles ──────────────────────────────────────────────────────────────
INSERT INTO roles (role_code, role_name) VALUES
    ('admin',     'Quản trị viên hệ thống'),
    ('candidate', 'Thí sinh'),
    ('proctor',   'Giám thị phòng thi'),
    ('reviewer',  'Giám khảo phúc khảo')
ON CONFLICT DO NOTHING;

-- ── 3. Exam Types ─────────────────────────────────────────────────────────
INSERT INTO exam_types (exam_type_code, exam_type_name, exam_type_description) VALUES
    ('HSK1',     'HSK Cấp 1',                'Tiếng Trung sơ cấp thấp – khoảng 150 từ vựng'),
    ('HSK2',     'HSK Cấp 2',                'Tiếng Trung sơ cấp – khoảng 300 từ vựng'),
    ('HSK3',     'HSK Cấp 3',                'Tiếng Trung trung cấp thấp – khoảng 600 từ vựng'),
    ('HSK4',     'HSK Cấp 4',                'Tiếng Trung trung cấp – khoảng 1200 từ vựng'),
    ('HSK5',     'HSK Cấp 5',                'Tiếng Trung trung cấp cao – khoảng 2500 từ vựng'),
    ('HSK6',     'HSK Cấp 6',                'Tiếng Trung nâng cao – trên 5000 từ vựng'),
    ('HSKK_SO',  'HSKK Sơ cấp',             'Bài thi nói tiếng Trung sơ cấp'),
    ('HSKK_TRUNG','HSKK Trung cấp',         'Bài thi nói tiếng Trung trung cấp'),
    ('HSKK_CAO', 'HSKK Cao cấp',            'Bài thi nói tiếng Trung cao cấp'),
    ('YCT4',     'YCT Cấp 4 – Thiếu nhi',   'Kỳ thi tiếng Trung dành cho học sinh phổ thông')
ON CONFLICT DO NOTHING;

-- ── 4. Provinces (34 tỉnh/thành sau sáp nhập 01/07/2025) ────────────────
INSERT INTO provinces (province_code, province_name) VALUES
    ('HN',  'Hà Nội'),
    ('HCM', 'Thành phố Hồ Chí Minh'),
    ('DN',  'Đà Nẵng'),
    ('HP',  'Hải Phòng'),
    ('CT',  'Cần Thơ'),
    ('NA',  'Nghệ An'),
    ('TH',  'Thanh Hóa'),
    ('QN',  'Quảng Ninh'),
    ('BD',  'Bình Dương'),
    ('DL',  'Đắk Lắk'),
    ('KH',  'Khánh Hòa'),
    ('LD',  'Lâm Đồng'),
    ('TT',  'Thừa Thiên Huế'),
    ('QNG', 'Quảng Ngãi'),
    ('BĐ',  'Bình Định'),
    ('AG',  'An Giang'),
    ('KG',  'Kiên Giang'),
    ('TV',  'Trà Vinh'),
    ('BT',  'Bình Thuận'),
    ('NĐ',  'Nam Định'),
    ('PY',  'Phú Yên'),
    ('GL',  'Gia Lai'),
    ('TG',  'Tiền Giang'),
    ('BL',  'Bạc Liêu'),
    ('ST',  'Sóc Trăng'),
    ('CM',  'Cà Mau'),
    ('VL',  'Vĩnh Long'),
    ('HG',  'Hà Giang'),
    ('LC',  'Lai Châu'),
    ('SL',  'Sơn La'),
    ('YB',  'Yên Bái'),
    ('PB',  'Phú Bình (Thái Nguyên)'),
    ('BG',  'Bắc Giang'),
    ('QT',  'Quảng Trị')
ON CONFLICT DO NOTHING;

-- ── 5. Wards (sample 100 wards across provinces 1-5) ─────────────────────
-- Hà Nội wards
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.type::VARCHAR
FROM provinces p,
     (VALUES
       ('HN-P01','Phường Hoàn Kiếm','phuong'),
       ('HN-P02','Phường Đống Đa','phuong'),
       ('HN-P03','Phường Ba Đình','phuong'),
       ('HN-P04','Phường Cầu Giấy','phuong'),
       ('HN-P05','Phường Hai Bà Trưng','phuong'),
       ('HN-X01','Xã Đông Anh','xa'),
       ('HN-X02','Xã Gia Lâm','xa'),
       ('HN-X03','Xã Thanh Trì','xa'),
       ('HN-X04','Xã Hoài Đức','xa'),
       ('HN-X05','Xã Thạch Thất','xa')
     ) AS w(code, name, type)
WHERE p.province_code = 'HN'
ON CONFLICT DO NOTHING;

-- TP.HCM wards
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.type::VARCHAR
FROM provinces p,
     (VALUES
       ('HCM-P01','Phường Bến Nghé','phuong'),
       ('HCM-P02','Phường Bến Thành','phuong'),
       ('HCM-P03','Phường Tân Định','phuong'),
       ('HCM-P04','Phường Phú Nhuận','phuong'),
       ('HCM-P05','Phường Gò Vấp','phuong'),
       ('HCM-X01','Xã Bình Chánh','xa'),
       ('HCM-X02','Xã Hóc Môn','xa'),
       ('HCM-X03','Xã Củ Chi','xa'),
       ('HCM-X04','Xã Nhà Bè','xa'),
       ('HCM-X05','Xã Cần Giờ','xa')
     ) AS w(code, name, type)
WHERE p.province_code = 'HCM'
ON CONFLICT DO NOTHING;

-- Nghệ An wards (location of HSK Vinh center)
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.type::VARCHAR
FROM provinces p,
     (VALUES
       ('NA-P01','Phường Lê Mao','phuong'),
       ('NA-P02','Phường Lê Lợi','phuong'),
       ('NA-P03','Phường Trường Thi','phuong'),
       ('NA-P04','Phường Bến Thủy','phuong'),
       ('NA-P05','Phường Hưng Bình','phuong'),
       ('NA-P06','Phường Hưng Phúc','phuong'),
       ('NA-P07','Phường Quang Trung','phuong'),
       ('NA-P08','Phường Đội Cung','phuong'),
       ('NA-X01','Xã Nghi Phú','xa'),
       ('NA-X02','Xã Hưng Lộc','xa'),
       ('NA-X03','Xã Nghi Kim','xa'),
       ('NA-X04','Xã Hưng Đông','xa'),
       ('NA-X05','Xã Nghi Ân','xa')
     ) AS w(code, name, type)
WHERE p.province_code = 'NA'
ON CONFLICT DO NOTHING;

-- Đà Nẵng wards
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.type::VARCHAR
FROM provinces p,
     (VALUES
       ('DN-P01','Phường Hải Châu','phuong'),
       ('DN-P02','Phường Thanh Khê','phuong'),
       ('DN-P03','Phường Sơn Trà','phuong'),
       ('DN-P04','Phường Ngũ Hành Sơn','phuong'),
       ('DN-P05','Phường Liên Chiểu','phuong'),
       ('DN-X01','Xã Hòa Vang','xa'),
       ('DN-X02','Xã Hòa Phú','xa')
     ) AS w(code, name, type)
WHERE p.province_code = 'DN'
ON CONFLICT DO NOTHING;

-- Hải Phòng wards
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.type::VARCHAR
FROM provinces p,
     (VALUES
       ('HP-P01','Phường Hồng Bàng','phuong'),
       ('HP-P02','Phường Ngô Quyền','phuong'),
       ('HP-P03','Phường Lê Chân','phuong'),
       ('HP-P04','Phường Kiến An','phuong'),
       ('HP-X01','Xã An Dương','xa'),
       ('HP-X02','Xã Thủy Nguyên','xa')
     ) AS w(code, name, type)
WHERE p.province_code = 'HP'
ON CONFLICT DO NOTHING;

-- ── 6. Exam Rooms ─────────────────────────────────────────────────────────
INSERT INTO exam_rooms (exam_room_name, exam_room_location, exam_room_capacity) VALUES
    ('Phòng thi số 1', 'Tầng 2, Nhà A, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 30),
    ('Phòng thi số 2', 'Tầng 2, Nhà A, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 30),
    ('Phòng thi số 3', 'Tầng 3, Nhà A, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 25),
    ('Phòng máy tính 01', 'Tầng 1, Nhà B, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 40),
    ('Phòng máy tính 02', 'Tầng 1, Nhà B, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 40)
ON CONFLICT DO NOTHING;

-- ── 7. Exam Sessions ──────────────────────────────────────────────────────
-- Assumes exam_type_id 1=HSK1, 2=HSK2, ...; exam_room_id 1..5
INSERT INTO exam_sessions
       (exam_session_exam_type_id, exam_session_exam_room_id,
        exam_session_date, exam_session_shift,
        exam_session_capacity, exam_session_fee,
        exam_session_registration_deadline, exam_session_status)
VALUES
    (1, 4, '2026-09-20', 'morning',   40, 350000, '2026-09-10 23:59:59+07', 'open'),
    (2, 4, '2026-09-20', 'afternoon', 40, 400000, '2026-09-10 23:59:59+07', 'open'),
    (3, 1, '2026-09-21', 'morning',   30, 500000, '2026-09-11 23:59:59+07', 'open'),
    (4, 2, '2026-09-21', 'afternoon', 30, 600000, '2026-09-11 23:59:59+07', 'open'),
    (5, 3, '2026-11-15', 'morning',   25, 700000, '2026-11-05 23:59:59+07', 'open'),
    (6, 3, '2026-11-15', 'afternoon', 25, 800000, '2026-11-05 23:59:59+07', 'open'),
    (7, 5, '2026-09-20', 'evening',   40, 350000, '2026-09-10 23:59:59+07', 'open'),
    (8, 5, '2026-11-15', 'morning',   40, 450000, '2026-11-05 23:59:59+07', 'open'),
    (9, 5, '2026-11-15', 'evening',   40, 500000, '2026-11-05 23:59:59+07', 'open'),
    (1, 4, '2026-12-13', 'morning',   40, 350000, '2026-12-03 23:59:59+07', 'open')
ON CONFLICT DO NOTHING;

-- ── 8. Exam Seats (auto-generate per session capacity) ────────────────────
-- Session 1 (40 seats)
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 1, gs, 'available' FROM generate_series(1, 40) gs
ON CONFLICT DO NOTHING;
-- Session 2 (40 seats)
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 2, gs, 'available' FROM generate_series(1, 40) gs
ON CONFLICT DO NOTHING;
-- Session 3 (30 seats)
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 3, gs, 'available' FROM generate_series(1, 30) gs
ON CONFLICT DO NOTHING;
-- Session 4 (30 seats)
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 4, gs, 'available' FROM generate_series(1, 30) gs
ON CONFLICT DO NOTHING;
-- Session 5 (25 seats)
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 5, gs, 'available' FROM generate_series(1, 25) gs
ON CONFLICT DO NOTHING;
-- Sessions 6-10
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 6, gs, 'available' FROM generate_series(1, 25) gs ON CONFLICT DO NOTHING;
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 7, gs, 'available' FROM generate_series(1, 40) gs ON CONFLICT DO NOTHING;
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 8, gs, 'available' FROM generate_series(1, 40) gs ON CONFLICT DO NOTHING;
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 9, gs, 'available' FROM generate_series(1, 40) gs ON CONFLICT DO NOTHING;
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT 10, gs, 'available' FROM generate_series(1, 40) gs ON CONFLICT DO NOTHING;

-- ── 9. Accounts (50 accounts: 2 admin + 3 staff + 45 candidates) ──────────
-- Admin & staff (password hash = bcrypt of "Password@123")
INSERT INTO accounts
       (account_role_id, account_username, account_password_hash, account_email, account_phone, account_status)
SELECT r.role_id, u.username, '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', u.email, u.phone, 'active'
FROM roles r,
     (VALUES
       ('admin','admin01', 'admin01@hskvinh.edu.vn', '02383855452'),
       ('admin','admin02', 'admin02@hskvinh.edu.vn', '02383855453'),
       ('proctor','proctor01', 'proctor01@hskvinh.edu.vn', '0912345601'),
       ('proctor','proctor02', 'proctor02@hskvinh.edu.vn', '0912345602'),
       ('reviewer','reviewer01', 'reviewer01@hskvinh.edu.vn', '0912345603')
     ) AS u(role_code, username, email, phone)
WHERE r.role_code = u.role_code
ON CONFLICT DO NOTHING;

-- 45 candidate accounts
INSERT INTO accounts
       (account_role_id, account_username, account_password_hash, account_email, account_phone, account_status)
SELECT r.role_id,
       'candidate' || gs,
       '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
       'candidate' || gs || '@gmail.com',
       '09' || LPAD(gs::TEXT, 8, '0'),
       'active'
FROM generate_series(1, 45) gs
CROSS JOIN roles r
WHERE r.role_code = 'candidate'
ON CONFLICT DO NOTHING;

-- ── 10. Candidates (50 profiles for candidate accounts) ────────────────────
INSERT INTO candidates
       (candidate_account_id, candidate_full_name, candidate_dob,
        candidate_gender, candidate_nationality)
SELECT a.account_id,
       CASE (a.account_id % 10)
           WHEN 0 THEN 'Nguyễn Thị Lan'
           WHEN 1 THEN 'Trần Văn Minh'
           WHEN 2 THEN 'Lê Thị Hoa'
           WHEN 3 THEN 'Phạm Văn Đức'
           WHEN 4 THEN 'Hoàng Thị Mai'
           WHEN 5 THEN 'Vũ Văn Hùng'
           WHEN 6 THEN 'Đặng Thị Thu'
           WHEN 7 THEN 'Bùi Văn Tú'
           WHEN 8 THEN 'Đỗ Thị Hương'
           ELSE        'Ngô Văn Dũng'
       END,
       ('1995-01-01'::DATE + (a.account_id % 3650) * '1 day'::INTERVAL)::DATE,
       CASE WHEN a.account_id % 2 = 0 THEN 'female' ELSE 'male' END,
       'Việt Nam'
FROM accounts a
JOIN roles r ON r.role_id = a.account_role_id
WHERE r.role_code = 'candidate' AND a.account_is_deleted = FALSE
ON CONFLICT DO NOTHING;

-- ── 11. Candidate Documents ────────────────────────────────────────────────
INSERT INTO candidate_documents
       (candidate_document_candidate_id, candidate_document_type,
        candidate_document_number, candidate_document_issue_date,
        candidate_document_issue_place, candidate_document_front_image_url,
        candidate_document_portrait_image_url,
        candidate_document_verification_status)
SELECT c.candidate_id,
       'cccd',
       '0' || LPAD(c.candidate_id::TEXT, 11, '5'),
       '2021-01-15',
       'Công an tỉnh Nghệ An',
       'https://storage.hskvinh.edu.vn/docs/front_' || c.candidate_id || '.jpg',
       'https://storage.hskvinh.edu.vn/docs/portrait_' || c.candidate_id || '.jpg',
       CASE WHEN c.candidate_id % 5 = 0 THEN 'verified'
            WHEN c.candidate_id % 5 = 1 THEN 'rejected'
            ELSE 'pending'
       END
FROM candidates c
ON CONFLICT DO NOTHING;

-- ── 12. Exam Registrations (20 registrations) ────────────────────────────
WITH cand_seats AS (
    SELECT
        c.candidate_id,
        s.exam_seat_id,
        s.exam_seat_exam_session_id,
        ROW_NUMBER() OVER (ORDER BY c.candidate_id) AS rn
    FROM candidates c
    CROSS JOIN LATERAL (
        SELECT exam_seat_id, exam_seat_exam_session_id
        FROM exam_seats
        WHERE exam_seat_exam_session_id = ((c.candidate_id % 4) + 1)
          AND exam_seat_status = 'available'
          AND exam_seat_is_deleted = FALSE
        ORDER BY exam_seat_number ASC
        LIMIT 1
    ) s
    LIMIT 20
)
INSERT INTO exam_registrations
       (exam_registration_candidate_id, exam_registration_exam_session_id,
        exam_registration_exam_seat_id, exam_registration_status)
SELECT candidate_id, exam_seat_exam_session_id, exam_seat_id,
       CASE WHEN rn % 3 = 0 THEN 'confirmed'
            WHEN rn % 3 = 1 THEN 'pending_payment'
            ELSE 'cancelled'
       END
FROM cand_seats
ON CONFLICT DO NOTHING;

-- Mark used seats as booked
UPDATE exam_seats
SET exam_seat_status = 'booked', exam_seat_updated_at = NOW()
WHERE exam_seat_id IN (
    SELECT exam_registration_exam_seat_id
    FROM exam_registrations
    WHERE exam_registration_status = 'confirmed'
);

-- ── 13. Payments (15 payments for confirmed/pending_payment registrations) ─
INSERT INTO payments
       (payment_exam_registration_id, payment_amount, payment_method,
        payment_transaction_ref, payment_status, payment_paid_at)
SELECT
    er.exam_registration_id,
    es.exam_session_fee,
    CASE (er.exam_registration_id % 3)
        WHEN 0 THEN 'vnpay'
        WHEN 1 THEN 'momo'
        ELSE 'bank_transfer'
    END,
    'TXN' || LPAD(er.exam_registration_id::TEXT, 10, '0'),
    CASE WHEN er.exam_registration_status = 'confirmed' THEN 'success'
         ELSE 'pending'
    END,
    CASE WHEN er.exam_registration_status = 'confirmed' THEN NOW() ELSE NULL END
FROM exam_registrations er
JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
WHERE er.exam_registration_status IN ('confirmed', 'pending_payment')
  AND er.exam_registration_is_deleted = FALSE
LIMIT 15
ON CONFLICT DO NOTHING;

-- ── 14. Exam Results (10 results for confirmed registrations) ─────────────
INSERT INTO exam_results
       (exam_result_exam_registration_id,
        exam_result_listening_score, exam_result_reading_score,
        exam_result_writing_score, exam_result_total_score,
        exam_result_status, exam_result_published_at)
SELECT
    er.exam_registration_id,
    (60 + (er.exam_registration_id % 30))::NUMERIC(5,1),
    (55 + (er.exam_registration_id % 35))::NUMERIC(5,1),
    (50 + (er.exam_registration_id % 40))::NUMERIC(5,1),
    (55 + (er.exam_registration_id % 35))::NUMERIC(6,1),
    CASE WHEN (er.exam_registration_id % 4) > 0 THEN 'pass' ELSE 'fail' END,
    NOW() - ((er.exam_registration_id % 7) || ' days')::INTERVAL
FROM exam_registrations er
WHERE er.exam_registration_status = 'confirmed'
  AND er.exam_registration_is_deleted = FALSE
LIMIT 10
ON CONFLICT DO NOTHING;

-- ── 15. Certificates (5 certificates for passing results) ────────────────
INSERT INTO certificates
       (certificate_exam_result_id, certificate_no,
        certificate_issue_date, certificate_verification_code, certificate_status)
SELECT
    r.exam_result_id,
    'HSK-VINH-2026-' || LPAD(r.exam_result_id::TEXT, 5, '0'),
    CURRENT_DATE,
    encode(digest('cert_' || r.exam_result_id::TEXT, 'sha256'), 'hex'),
    'issued'
FROM exam_results r
WHERE r.exam_result_status = 'pass' AND r.exam_result_is_deleted = FALSE
LIMIT 5
ON CONFLICT DO NOTHING;

-- ── 16. Notifications (20 notifications) ─────────────────────────────────
INSERT INTO notifications
       (notification_account_id, notification_channel, notification_content, notification_status)
SELECT
    a.account_id,
    CASE (a.account_id % 4)
        WHEN 0 THEN 'email'
        WHEN 1 THEN 'sms'
        WHEN 2 THEN 'zalo'
        ELSE 'push'
    END,
    CASE (a.account_id % 3)
        WHEN 0 THEN 'Đăng ký thi của bạn đã được xác nhận. Vui lòng thanh toán lệ phí trước hạn.'
        WHEN 1 THEN 'Kết quả thi HSK của bạn đã được công bố. Đăng nhập để xem chi tiết.'
        ELSE        'Chứng chỉ HSK của bạn đã sẵn sàng. Vui lòng kiểm tra và đăng ký nhận về địa chỉ.'
    END,
    CASE WHEN a.account_id % 5 = 0 THEN 'sent' ELSE 'pending' END
FROM accounts a
JOIN roles r ON r.role_id = a.account_role_id
WHERE r.role_code = 'candidate'
LIMIT 20
ON CONFLICT DO NOTHING;

-- ── 17. Audit Logs (30 entries) ───────────────────────────────────────────
INSERT INTO audit_logs
       (audit_log_actor_account_id, audit_log_action, audit_log_entity_table,
        audit_log_entity_id, audit_log_old_value, audit_log_new_value)
SELECT
    (SELECT account_id FROM accounts WHERE account_username = 'admin01' LIMIT 1),
    CASE (gs % 5)
        WHEN 0 THEN 'CREATE'
        WHEN 1 THEN 'UPDATE'
        WHEN 2 THEN 'VERIFY_DOCUMENT'
        WHEN 3 THEN 'PUBLISH_RESULT'
        ELSE 'CANCEL_REGISTRATION'
    END,
    CASE (gs % 3)
        WHEN 0 THEN 'exam_registrations'
        WHEN 1 THEN 'candidate_documents'
        ELSE        'exam_results'
    END,
    gs,
    NULL,
    jsonb_build_object('action_at', now(), 'actor', 'admin01', 'seq', gs)
FROM generate_series(1, 30) gs
ON CONFLICT DO NOTHING;

-- ── 18. Reset sequences ───────────────────────────────────────────────────
SELECT setval(pg_get_serial_sequence('roles',                   'role_id'),            (SELECT COALESCE(MAX(role_id), 1) FROM roles));
SELECT setval(pg_get_serial_sequence('exam_types',              'exam_type_id'),        (SELECT COALESCE(MAX(exam_type_id), 1) FROM exam_types));
SELECT setval(pg_get_serial_sequence('provinces',               'province_id'),         (SELECT COALESCE(MAX(province_id), 1) FROM provinces));
SELECT setval(pg_get_serial_sequence('wards',                   'ward_id'),             (SELECT COALESCE(MAX(ward_id), 1) FROM wards));
SELECT setval(pg_get_serial_sequence('accounts',                'account_id'),          (SELECT COALESCE(MAX(account_id), 1) FROM accounts));
SELECT setval(pg_get_serial_sequence('candidates',              'candidate_id'),        (SELECT COALESCE(MAX(candidate_id), 1) FROM candidates));
SELECT setval(pg_get_serial_sequence('candidate_documents',     'candidate_document_id'),(SELECT COALESCE(MAX(candidate_document_id), 1) FROM candidate_documents));
SELECT setval(pg_get_serial_sequence('exam_rooms',              'exam_room_id'),        (SELECT COALESCE(MAX(exam_room_id), 1) FROM exam_rooms));
SELECT setval(pg_get_serial_sequence('exam_sessions',           'exam_session_id'),     (SELECT COALESCE(MAX(exam_session_id), 1) FROM exam_sessions));
SELECT setval(pg_get_serial_sequence('exam_seats',              'exam_seat_id'),        (SELECT COALESCE(MAX(exam_seat_id), 1) FROM exam_seats));
SELECT setval(pg_get_serial_sequence('exam_registrations',      'exam_registration_id'),(SELECT COALESCE(MAX(exam_registration_id), 1) FROM exam_registrations));
SELECT setval(pg_get_serial_sequence('payments',                'payment_id'),          (SELECT COALESCE(MAX(payment_id), 1) FROM payments));
SELECT setval(pg_get_serial_sequence('exam_results',            'exam_result_id'),      (SELECT COALESCE(MAX(exam_result_id), 1) FROM exam_results));
SELECT setval(pg_get_serial_sequence('certificates',            'certificate_id'),      (SELECT COALESCE(MAX(certificate_id), 1) FROM certificates));
