-- =============================================================================
-- 002_seed_data.sql
-- HSK Vinh Registration System – Realistic Vietnamese seed data
-- Depends on: 001_schema.sql
-- Password hash = bcrypt("Password@123", cost=10)
-- =============================================================================

-- ── 1. Roles ─────────────────────────────────────────────────────────────────
INSERT INTO roles (role_code, role_name) VALUES
    ('admin',     'Quản trị viên hệ thống'),
    ('candidate', 'Thí sinh'),
    ('proctor',   'Giám thị phòng thi'),
    ('reviewer',  'Giám khảo phúc khảo')
ON CONFLICT (role_code) DO NOTHING;

-- ── 2. Exam Types ────────────────────────────────────────────────────────────
INSERT INTO exam_types (exam_type_code, exam_type_name, exam_type_description) VALUES
    ('HSK1',      'HSK Cấp 1',          'Tiếng Trung sơ cấp thấp – khoảng 150 từ vựng'),
    ('HSK2',      'HSK Cấp 2',          'Tiếng Trung sơ cấp – khoảng 300 từ vựng'),
    ('HSK3',      'HSK Cấp 3',          'Tiếng Trung trung cấp thấp – khoảng 600 từ vựng'),
    ('HSK4',      'HSK Cấp 4',          'Tiếng Trung trung cấp – khoảng 1200 từ vựng'),
    ('HSK5',      'HSK Cấp 5',          'Tiếng Trung trung cấp cao – khoảng 2500 từ vựng'),
    ('HSK6',      'HSK Cấp 6',          'Tiếng Trung nâng cao – trên 5000 từ vựng'),
    ('HSKK_SO',   'HSKK Sơ cấp',        'Bài thi nói tiếng Trung sơ cấp'),
    ('HSKK_TRUNG','HSKK Trung cấp',     'Bài thi nói tiếng Trung trung cấp'),
    ('HSKK_CAO',  'HSKK Cao cấp',       'Bài thi nói tiếng Trung cao cấp'),
    ('YCT4',      'YCT Cấp 4 – Thiếu nhi', 'Kỳ thi tiếng Trung dành cho học sinh phổ thông')
ON CONFLICT (exam_type_code) DO NOTHING;

-- ── 3. Provinces (34 tỉnh/thành sau sáp nhập 01/07/2025) ───────────────────
INSERT INTO provinces (province_code, province_name) VALUES
    ('HN',   'Hà Nội'),
    ('HCM',  'Thành phố Hồ Chí Minh'),
    ('DN',   'Đà Nẵng'),
    ('HP',   'Hải Phòng'),
    ('CT',   'Cần Thơ'),
    ('NA',   'Nghệ An'),
    ('TH',   'Thanh Hóa'),
    ('QN',   'Quảng Ninh'),
    ('BD',   'Bình Dương'),
    ('DL',   'Đắk Lắk'),
    ('KH',   'Khánh Hòa'),
    ('LD',   'Lâm Đồng'),
    ('TTH',  'Thừa Thiên Huế'),
    ('QNG',  'Quảng Ngãi'),
    ('BDH',  'Bình Định'),
    ('AG',   'An Giang'),
    ('KG',   'Kiên Giang'),
    ('TV',   'Trà Vinh'),
    ('BT',   'Bình Thuận'),
    ('NDH',  'Nam Định'),
    ('PY',   'Phú Yên'),
    ('GL',   'Gia Lai'),
    ('TG',   'Tiền Giang'),
    ('BL',   'Bạc Liêu'),
    ('ST',   'Sóc Trăng'),
    ('CM',   'Cà Mau'),
    ('VL',   'Vĩnh Long'),
    ('HG',   'Hà Giang'),
    ('LC',   'Lai Châu'),
    ('SL',   'Sơn La'),
    ('YB',   'Yên Bái'),
    ('TNI',  'Thái Nguyên'),
    ('BG',   'Bắc Giang'),
    ('QT',   'Quảng Trị')
ON CONFLICT (province_code) DO NOTHING;

-- ── 4. Wards ────────────────────────────────────────────────────────────────
-- Nghệ An (home of HSK Vinh center)
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.wtype
FROM provinces p
CROSS JOIN (VALUES
    ('NA-P01', 'Phường Lê Mao',     'phuong'),
    ('NA-P02', 'Phường Lê Lợi',     'phuong'),
    ('NA-P03', 'Phường Trường Thi',  'phuong'),
    ('NA-P04', 'Phường Bến Thủy',   'phuong'),
    ('NA-P05', 'Phường Hưng Bình',  'phuong'),
    ('NA-P06', 'Phường Hưng Phúc',  'phuong'),
    ('NA-P07', 'Phường Quang Trung', 'phuong'),
    ('NA-P08', 'Phường Đội Cung',   'phuong'),
    ('NA-X01', 'Xã Nghi Phú',       'xa'),
    ('NA-X02', 'Xã Hưng Lộc',       'xa'),
    ('NA-X03', 'Xã Nghi Kim',        'xa'),
    ('NA-X04', 'Xã Hưng Đông',      'xa'),
    ('NA-X05', 'Xã Nghi Ân',        'xa')
) AS w(code, name, wtype)
WHERE p.province_code = 'NA'
ON CONFLICT (ward_code) DO NOTHING;

-- Hà Nội
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.wtype
FROM provinces p
CROSS JOIN (VALUES
    ('HN-P01', 'Phường Hoàn Kiếm',    'phuong'),
    ('HN-P02', 'Phường Đống Đa',       'phuong'),
    ('HN-P03', 'Phường Ba Đình',        'phuong'),
    ('HN-P04', 'Phường Cầu Giấy',      'phuong'),
    ('HN-P05', 'Phường Hai Bà Trưng',  'phuong'),
    ('HN-X01', 'Xã Đông Anh',          'xa'),
    ('HN-X02', 'Xã Gia Lâm',           'xa')
) AS w(code, name, wtype)
WHERE p.province_code = 'HN'
ON CONFLICT (ward_code) DO NOTHING;

-- TP.HCM
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.wtype
FROM provinces p
CROSS JOIN (VALUES
    ('HCM-P01', 'Phường Bến Nghé',  'phuong'),
    ('HCM-P02', 'Phường Bến Thành', 'phuong'),
    ('HCM-P03', 'Phường Tân Định',  'phuong'),
    ('HCM-P04', 'Phường Phú Nhuận', 'phuong'),
    ('HCM-P05', 'Phường Gò Vấp',    'phuong'),
    ('HCM-X01', 'Xã Bình Chánh',    'xa'),
    ('HCM-X02', 'Xã Củ Chi',        'xa')
) AS w(code, name, wtype)
WHERE p.province_code = 'HCM'
ON CONFLICT (ward_code) DO NOTHING;

-- Đà Nẵng
INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type)
SELECT p.province_id, w.code, w.name, w.wtype
FROM provinces p
CROSS JOIN (VALUES
    ('DN-P01', 'Phường Hải Châu',       'phuong'),
    ('DN-P02', 'Phường Thanh Khê',      'phuong'),
    ('DN-P03', 'Phường Sơn Trà',        'phuong'),
    ('DN-P04', 'Phường Ngũ Hành Sơn',  'phuong'),
    ('DN-X01', 'Xã Hòa Vang',           'xa')
) AS w(code, name, wtype)
WHERE p.province_code = 'DN'
ON CONFLICT (ward_code) DO NOTHING;

-- ── 5. Exam Rooms ────────────────────────────────────────────────────────────
INSERT INTO exam_rooms (exam_room_name, exam_room_location, exam_room_capacity) VALUES
    ('Phòng thi 101',       'Tầng 1, Nhà A2, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 30),
    ('Phòng thi 102',       'Tầng 1, Nhà A2, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 30),
    ('Phòng thi 201',       'Tầng 2, Nhà A2, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 25),
    ('Phòng máy tính 301',  'Tầng 3, Nhà B1, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 40),
    ('Phòng máy tính 302',  'Tầng 3, Nhà B1, Trường Đại học Vinh, 182 Lê Duẩn, Tp. Vinh, Nghệ An', 40);

-- ── 6. Exam Sessions ─────────────────────────────────────────────────────────
INSERT INTO exam_sessions (
    exam_session_exam_type_id,
    exam_session_exam_room_id,
    exam_session_date,
    exam_session_shift,
    exam_session_capacity,
    exam_session_fee,
    exam_session_registration_deadline,
    exam_session_status
)
SELECT
    et.exam_type_id,
    er.exam_room_id,
    s.sess_date::DATE,
    s.shift,
    er.exam_room_capacity,
    s.fee,
    (s.sess_date::DATE - INTERVAL '10 days' + TIME '23:59:59')::TIMESTAMPTZ,
    'open'
FROM (VALUES
    ('HSK1',      'Phòng máy tính 301', '2026-09-20', 'morning',   350000),
    ('HSK2',      'Phòng máy tính 301', '2026-09-20', 'afternoon', 400000),
    ('HSK3',      'Phòng thi 101',      '2026-09-21', 'morning',   500000),
    ('HSK4',      'Phòng thi 102',      '2026-09-21', 'afternoon', 600000),
    ('HSK5',      'Phòng thi 201',      '2026-11-15', 'morning',   700000),
    ('HSK6',      'Phòng thi 201',      '2026-11-15', 'afternoon', 800000),
    ('HSKK_SO',   'Phòng máy tính 302', '2026-09-20', 'evening',   350000),
    ('HSKK_TRUNG','Phòng máy tính 302', '2026-11-15', 'morning',   450000),
    ('HSKK_CAO',  'Phòng máy tính 302', '2026-11-15', 'evening',   500000),
    ('HSK1',      'Phòng máy tính 301', '2026-12-13', 'morning',   350000)
) AS s(type_code, room_name, sess_date, shift, fee)
JOIN exam_types et ON et.exam_type_code  = s.type_code  AND et.exam_type_is_deleted  = FALSE
JOIN exam_rooms er ON er.exam_room_name  = s.room_name  AND er.exam_room_is_deleted  = FALSE;

-- ── 7. Exam Seats ────────────────────────────────────────────────────────────
-- Generate available seats for every session based on its capacity
INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
SELECT
    es.exam_session_id,
    seat_no,
    'available'
FROM exam_sessions es
CROSS JOIN generate_series(1, es.exam_session_capacity) AS seat_no
WHERE es.exam_session_is_deleted = FALSE
ON CONFLICT (exam_seat_exam_session_id, exam_seat_number) DO NOTHING;

-- ── 8. Accounts ──────────────────────────────────────────────────────────────
-- Admins & staff (bcrypt hash of "Password@123")
INSERT INTO accounts
    (account_role_id, account_username, account_password_hash, account_email, account_phone, account_status)
SELECT r.role_id, u.username,
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
    u.email, u.phone, 'active'
FROM roles r
JOIN (VALUES
    ('admin',    'admin01',    'admin01@hskvinh.edu.vn',    '02383855452'),
    ('admin',    'admin02',    'admin02@hskvinh.edu.vn',    '02383855453'),
    ('proctor',  'proctor01',  'proctor01@hskvinh.edu.vn',  '0912345601'),
    ('proctor',  'proctor02',  'proctor02@hskvinh.edu.vn',  '0912345602'),
    ('reviewer', 'reviewer01', 'reviewer01@hskvinh.edu.vn', '0912345603')
) AS u(role_code, username, email, phone)
  ON r.role_code = u.role_code AND r.role_is_deleted = FALSE
ON CONFLICT (account_username) DO NOTHING;

-- 45 candidate accounts
INSERT INTO accounts
    (account_role_id, account_username, account_password_hash, account_email, account_phone, account_status)
SELECT r.role_id,
    'candidate' || gs,
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
    'candidate' || gs || '@gmail.com',
    '09' || LPAD(gs::TEXT, 8, '0'),
    'active'
FROM generate_series(1, 45) AS gs
CROSS JOIN roles r
WHERE r.role_code = 'candidate' AND r.role_is_deleted = FALSE
ON CONFLICT (account_username) DO NOTHING;

-- ── 9. Candidate Profiles ────────────────────────────────────────────────────
INSERT INTO candidates
    (candidate_account_id, candidate_full_name, candidate_dob, candidate_gender, candidate_nationality)
SELECT
    a.account_id,
    (ARRAY[
        'Nguyễn Thị Lan',   'Trần Văn Minh',    'Lê Thị Hoa',
        'Phạm Văn Đức',     'Hoàng Thị Mai',    'Vũ Văn Hùng',
        'Đặng Thị Thu',     'Bùi Văn Tú',       'Đỗ Thị Hương',
        'Ngô Văn Dũng',     'Lý Thị Bình',      'Trịnh Văn An',
        'Dương Thị Ngọc',   'Phan Văn Khoa',    'Cao Thị Thanh'
    ])[((row_number() OVER (ORDER BY a.account_id) - 1) % 15) + 1],
    ('1995-01-01'::DATE + ((row_number() OVER (ORDER BY a.account_id) * 73) % 3650) * '1 day'::INTERVAL)::DATE,
    CASE WHEN row_number() OVER (ORDER BY a.account_id) % 2 = 0 THEN 'female' ELSE 'male' END,
    'Việt Nam'
FROM accounts a
JOIN roles r ON r.role_id = a.account_role_id
WHERE r.role_code = 'candidate' AND a.account_is_deleted = FALSE
ON CONFLICT (candidate_account_id) DO NOTHING;

-- ── 10. Candidate Documents ──────────────────────────────────────────────────
INSERT INTO candidate_documents (
    candidate_document_candidate_id,
    candidate_document_type,
    candidate_document_number,
    candidate_document_issue_date,
    candidate_document_issue_place,
    candidate_document_front_image_url,
    candidate_document_portrait_image_url,
    candidate_document_verification_status
)
SELECT
    c.candidate_id,
    'cccd',
    LPAD(c.candidate_id::TEXT, 12, '0'),
    '2021-01-15',
    'Công an tỉnh Nghệ An',
    'https://storage.hskvinh.edu.vn/docs/front_'    || c.candidate_id || '.jpg',
    'https://storage.hskvinh.edu.vn/docs/portrait_' || c.candidate_id || '.jpg',
    CASE c.candidate_id % 5
        WHEN 0 THEN 'verified'
        WHEN 1 THEN 'rejected'
        ELSE        'pending'
    END
FROM candidates c
WHERE c.candidate_is_deleted = FALSE;

-- ── 11. Exam Registrations (20 sample registrations) ─────────────────────────
-- Simple approach: join first 20 candidates with numbered available seats from sessions 1-4.
WITH candidate_seq AS (
    SELECT candidate_id, ROW_NUMBER() OVER (ORDER BY candidate_id) AS rn
    FROM candidates
    WHERE candidate_is_deleted = FALSE
    LIMIT 20
),
session_seq AS (
    SELECT exam_session_id, ROW_NUMBER() OVER (ORDER BY exam_session_id) AS rn
    FROM exam_sessions
    WHERE exam_session_is_deleted = FALSE
    LIMIT 4
),
available_seats AS (
    SELECT
        s.exam_seat_id,
        s.exam_seat_exam_session_id,
        ROW_NUMBER() OVER (
            PARTITION BY s.exam_seat_exam_session_id
            ORDER BY s.exam_seat_number
        ) AS seat_rn
    FROM exam_seats s
    WHERE s.exam_seat_status = 'available'
      AND s.exam_seat_is_deleted = FALSE
)
INSERT INTO exam_registrations
    (exam_registration_candidate_id, exam_registration_exam_session_id,
     exam_registration_exam_seat_id, exam_registration_status)
SELECT
    cs.candidate_id,
    ss.exam_session_id,
    av.exam_seat_id,
    CASE cs.rn % 3
        WHEN 0 THEN 'confirmed'
        WHEN 1 THEN 'pending_payment'
        ELSE        'cancelled'
    END
FROM candidate_seq cs
JOIN session_seq ss ON ss.rn = ((cs.rn - 1) % 4) + 1
JOIN available_seats av
    ON av.exam_seat_exam_session_id = ss.exam_session_id
   AND av.seat_rn = ((cs.rn - 1) / 4) + 1
WHERE av.exam_seat_id IS NOT NULL;

-- Mark booked seats
UPDATE exam_seats
SET exam_seat_status     = 'booked',
    exam_seat_updated_at = NOW()
WHERE exam_seat_id IN (
    SELECT exam_registration_exam_seat_id
    FROM exam_registrations
    WHERE exam_registration_status = 'confirmed'
);

-- ── 12. Payments ─────────────────────────────────────────────────────────────
INSERT INTO payments
    (payment_exam_registration_id, payment_amount, payment_method,
     payment_transaction_ref, payment_status, payment_paid_at)
SELECT
    er.exam_registration_id,
    es.exam_session_fee,
    (ARRAY['vnpay','momo','bank_transfer'])[er.exam_registration_id % 3 + 1],
    'TXN' || LPAD(er.exam_registration_id::TEXT, 10, '0'),
    CASE er.exam_registration_status
        WHEN 'confirmed'      THEN 'success'
        WHEN 'pending_payment' THEN 'pending'
        ELSE                       'failed'
    END,
    CASE er.exam_registration_status
        WHEN 'confirmed' THEN NOW() - (er.exam_registration_id % 7 || ' days')::INTERVAL
        ELSE NULL
    END
FROM exam_registrations er
JOIN exam_sessions es
    ON es.exam_session_id = er.exam_registration_exam_session_id
WHERE er.exam_registration_is_deleted = FALSE;

-- ── 13. Exam Results (for confirmed registrations) ───────────────────────────
INSERT INTO exam_results (
    exam_result_exam_registration_id,
    exam_result_listening_score,
    exam_result_reading_score,
    exam_result_writing_score,
    exam_result_total_score,
    exam_result_status,
    exam_result_published_at
)
SELECT
    er.exam_registration_id,
    (60 + (er.exam_registration_id % 30))::NUMERIC(5,1),
    (55 + (er.exam_registration_id % 35))::NUMERIC(5,1),
    (50 + (er.exam_registration_id % 40))::NUMERIC(5,1),
    ((60 + (er.exam_registration_id % 30) +
      55 + (er.exam_registration_id % 35) +
      50 + (er.exam_registration_id % 40)) / 3.0)::NUMERIC(6,1),
    CASE WHEN er.exam_registration_id % 4 > 0 THEN 'pass' ELSE 'fail' END,
    NOW() - (er.exam_registration_id % 7 || ' days')::INTERVAL
FROM exam_registrations er
WHERE er.exam_registration_status = 'confirmed'
  AND er.exam_registration_is_deleted = FALSE;

-- ── 14. Certificates (for passing results) ───────────────────────────────────
INSERT INTO certificates (
    certificate_exam_result_id,
    certificate_no,
    certificate_issue_date,
    certificate_verification_code,
    certificate_status
)
SELECT
    r.exam_result_id,
    'CERT-VINH-2026-' || LPAD(r.exam_result_id::TEXT, 5, '0'),
    CURRENT_DATE,
    encode(digest('cert_secret_' || r.exam_result_id::TEXT, 'sha256'), 'hex'),
    'issued'
FROM exam_results r
WHERE r.exam_result_status = 'pass'
  AND r.exam_result_is_deleted = FALSE
ON CONFLICT (certificate_exam_result_id) DO NOTHING;

-- ── 15. Notifications ────────────────────────────────────────────────────────
INSERT INTO notifications
    (notification_account_id, notification_channel, notification_content, notification_status)
SELECT
    a.account_id,
    (ARRAY['email','sms','zalo','push'])[((row_number() OVER (ORDER BY a.account_id)) % 4) + 1],
    CASE (row_number() OVER (ORDER BY a.account_id)) % 3
        WHEN 0 THEN 'Đăng ký thi của bạn đã được xác nhận. Vui lòng thanh toán lệ phí trước hạn.'
        WHEN 1 THEN 'Kết quả thi HSK của bạn đã được công bố. Đăng nhập để xem chi tiết.'
        ELSE        'Chứng chỉ HSK của bạn đã sẵn sàng. Vui lòng đăng ký nhận về địa chỉ.'
    END,
    CASE (row_number() OVER (ORDER BY a.account_id)) % 5
        WHEN 0 THEN 'sent'
        ELSE        'pending'
    END
FROM accounts a
JOIN roles r ON r.role_id = a.account_role_id
WHERE r.role_code = 'candidate'
LIMIT 20;

-- ── 16. Audit Logs ───────────────────────────────────────────────────────────
INSERT INTO audit_logs
    (audit_log_actor_account_id, audit_log_action, audit_log_entity_table,
     audit_log_entity_id, audit_log_old_value, audit_log_new_value)
SELECT
    (SELECT account_id FROM accounts WHERE account_username = 'admin01'),
    (ARRAY['CREATE','UPDATE','VERIFY_DOCUMENT','PUBLISH_RESULT','CONFIRM_REGISTRATION'])[gs % 5 + 1],
    (ARRAY['exam_registrations','candidate_documents','exam_results'])[gs % 3 + 1],
    gs,
    NULL,
    jsonb_build_object('seq', gs, 'actor', 'admin01', 'at', now())
FROM generate_series(1, 30) AS gs;
