-- =====================================================================
-- HSK-VINH | Hệ thống đăng ký thi HSK máy tính - Database Schema
-- PostgreSQL 16 | 3NF | Soft-delete | Prefixed columns | Auto-increment ID
-- =====================================================================

-- ---------------------------------------------------------------------
-- 0. EXTENSIONS
-- ---------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- dùng cho digest() sinh certificate_verification_code

-- ---------------------------------------------------------------------
-- 0.1 GENERIC TRIGGER FUNCTION: tự động cập nhật cột *_updated_at
--     Dùng chung cho toàn bộ 19 bảng, tên cột truyền qua TG_ARGV[0]
--     (tránh phải viết 19 hàm trigger riêng lẻ mà vẫn giữ đúng kiểu dữ liệu
--      nhờ round-trip qua jsonb + jsonb_populate_record)
-- ---------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_touch_updated_at() RETURNS trigger AS $$
DECLARE
    v_col  text := TG_ARGV[0];
    v_data jsonb;
BEGIN
    v_data := to_jsonb(NEW) || jsonb_build_object(v_col, now());
    NEW := jsonb_populate_record(NEW, v_data);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =====================================================================
-- 1. BẢNG DANH MỤC / THAM CHIẾU (reference tables)
-- =====================================================================

-- 1.1 roles: vai trò hệ thống (thí sinh, admin, giám thị, giám khảo phúc khảo...)
CREATE TABLE roles (
    role_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    role_code       VARCHAR(30)  NOT NULL,
    role_name       VARCHAR(100) NOT NULL,
    role_is_deleted BOOLEAN      NOT NULL DEFAULT FALSE,
    role_deleted_at TIMESTAMPTZ,
    role_created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    role_updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 1.2 exam_types: loại kỳ thi (HSK 1-6, HSKK sơ/trung/cao cấp)
CREATE TABLE exam_types (
    exam_type_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_type_code        VARCHAR(20)  NOT NULL,
    exam_type_name        VARCHAR(100) NOT NULL,
    exam_type_description TEXT,
    exam_type_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    exam_type_deleted_at  TIMESTAMPTZ,
    exam_type_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    exam_type_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 1.3 provinces / 1.4 wards
-- LƯU Ý QUAN TRỌNG: Từ 01/07/2025, Việt Nam chính thức bỏ cấp huyện, vận hành
-- chính quyền địa phương 2 CẤP (tỉnh/thành phố -> xã/phường/đặc khu), cả nước
-- còn 34 tỉnh/thành sau sáp nhập. Vì vậy địa chỉ hành chính ở đây chỉ chuẩn
-- hoá 2 cấp (provinces -> wards), KHÔNG có bảng districts như tài liệu cũ
-- trước cải cách. Đây là điểm nhiều đồ án/đề tài cũ (kể cả hệ thống TNU tham
-- chiếu) có thể vẫn còn dùng mô hình 3 cấp lỗi thời -> nên chỉnh lại.
CREATE TABLE provinces (
    province_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    province_code        VARCHAR(20)  NOT NULL,
    province_name        VARCHAR(100) NOT NULL,
    province_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    province_deleted_at  TIMESTAMPTZ,
    province_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    province_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE wards (
    ward_id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ward_province_id BIGINT       NOT NULL REFERENCES provinces (province_id),
    ward_code        VARCHAR(20)  NOT NULL,
    ward_name        VARCHAR(100) NOT NULL,
    ward_type         VARCHAR(10) NOT NULL DEFAULT 'xa'
                           CHECK (ward_type IN ('xa','phuong','dac_khu')), -- xã / phường / đặc khu
    ward_is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE,
    ward_deleted_at  TIMESTAMPTZ,
    ward_created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ward_updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- =====================================================================
-- 2. ĐỊNH DANH & HỒ SƠ THÍ SINH (identity)
-- =====================================================================

-- 2.1 accounts: tài khoản đăng nhập dùng chung cho thí sinh lẫn nhân sự
--     (admin, giám thị, giám khảo phúc khảo) -> tránh 2 bảng auth trùng lặp
CREATE TABLE accounts (
    account_id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_role_id        BIGINT       NOT NULL REFERENCES roles (role_id),
    account_username       VARCHAR(50)  NOT NULL,
    account_password_hash  VARCHAR(255) NOT NULL,
    account_email          VARCHAR(150) NOT NULL,
    account_phone          VARCHAR(20),
    account_status         VARCHAR(20)  NOT NULL DEFAULT 'active'
                                CHECK (account_status IN ('active','locked','pending')),
    account_last_login_at  TIMESTAMPTZ,
    account_is_deleted     BOOLEAN      NOT NULL DEFAULT FALSE,
    account_deleted_at     TIMESTAMPTZ,
    account_created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    account_updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 2.2 candidates: hồ sơ cá nhân thí sinh (1-1 với accounts)
CREATE TABLE candidates (
    candidate_id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    candidate_account_id          BIGINT       NOT NULL REFERENCES accounts (account_id),
    candidate_full_name           VARCHAR(150) NOT NULL,
    candidate_full_name_cn        VARCHAR(150),
    candidate_dob                 DATE         NOT NULL,
    candidate_gender              VARCHAR(10)  NOT NULL
                                       CHECK (candidate_gender IN ('male','female','other')),
    candidate_ethnicity           VARCHAR(50),
    candidate_religion            VARCHAR(50),
    candidate_birthplace          VARCHAR(150),
    candidate_nationality         VARCHAR(50)  NOT NULL DEFAULT 'Việt Nam',
    candidate_mother_tongue       VARCHAR(50),
    candidate_chinese_study_years SMALLINT     CHECK (candidate_chinese_study_years >= 0),
    candidate_is_deleted          BOOLEAN      NOT NULL DEFAULT FALSE,
    candidate_deleted_at          TIMESTAMPTZ,
    candidate_created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    candidate_updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 2.3 candidate_documents: CCCD/hộ chiếu + ảnh chân dung + địa chỉ thường trú
--     Tách khỏi candidates vì đây là nhóm thuộc tính có thể thay đổi độc lập
--     (thí sinh đổi CCCD, cập nhật lại ảnh) mà không đụng vào hồ sơ gốc -> 3NF
CREATE TABLE candidate_documents (
    candidate_document_id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    candidate_document_candidate_id       BIGINT       NOT NULL REFERENCES candidates (candidate_id),
    candidate_document_type               VARCHAR(20)  NOT NULL
                                               CHECK (candidate_document_type IN ('cccd','passport')),
    candidate_document_number             VARCHAR(30)  NOT NULL,
    candidate_document_issue_date         DATE         NOT NULL,
    candidate_document_issue_place        VARCHAR(150) NOT NULL,
    candidate_document_front_image_url    VARCHAR(255) NOT NULL,
    candidate_document_back_image_url     VARCHAR(255),
    candidate_document_portrait_image_url VARCHAR(255) NOT NULL,
    candidate_document_verification_status VARCHAR(20) NOT NULL DEFAULT 'pending'
                                               CHECK (candidate_document_verification_status IN ('pending','verified','rejected')),
    candidate_document_ward_id            BIGINT       REFERENCES wards (ward_id),
    candidate_document_address_detail     VARCHAR(255),
    candidate_document_is_deleted         BOOLEAN      NOT NULL DEFAULT FALSE,
    candidate_document_deleted_at         TIMESTAMPTZ,
    candidate_document_created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    candidate_document_updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- =====================================================================
-- 3. NGHIỆP VỤ TỔ CHỨC THI (exam operations)
-- =====================================================================

-- 3.1 exam_rooms: phòng thi vật lý tại trung tâm
CREATE TABLE exam_rooms (
    exam_room_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_room_name        VARCHAR(100) NOT NULL,
    exam_room_location     VARCHAR(255),
    exam_room_capacity     INT          NOT NULL CHECK (exam_room_capacity > 0),
    exam_room_is_deleted   BOOLEAN      NOT NULL DEFAULT FALSE,
    exam_room_deleted_at   TIMESTAMPTZ,
    exam_room_created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    exam_room_updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 3.2 exam_sessions: một ca thi cụ thể = loại kỳ thi + phòng + ngày + buổi
CREATE TABLE exam_sessions (
    exam_session_id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_session_exam_type_id           BIGINT      NOT NULL REFERENCES exam_types (exam_type_id),
    exam_session_exam_room_id           BIGINT      NOT NULL REFERENCES exam_rooms (exam_room_id),
    exam_session_date                   DATE        NOT NULL,
    exam_session_shift                  VARCHAR(10) NOT NULL
                                             CHECK (exam_session_shift IN ('morning','afternoon','evening')),
    exam_session_capacity               INT         NOT NULL CHECK (exam_session_capacity > 0),
    exam_session_fee                    NUMERIC(12,0) NOT NULL CHECK (exam_session_fee >= 0),
    exam_session_registration_deadline  TIMESTAMPTZ NOT NULL,
    exam_session_status                 VARCHAR(20) NOT NULL DEFAULT 'open'
                                             CHECK (exam_session_status IN ('open','closed','cancelled')),
    exam_session_is_deleted             BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_session_deleted_at             TIMESTAMPTZ,
    exam_session_created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_session_updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3.3 exam_seats: "kho chỗ ngồi" được sinh sẵn theo capacity của từng ca thi.
--     Đây là chìa khoá cho concurrency: thay vì lock nguyên dòng exam_sessions
--     (điểm nghẽn khi hàng chục nghìn request cùng đăng ký 1 ca), mỗi request
--     chỉ SELECT ... FOR UPDATE SKIP LOCKED một dòng ghế trống -> các transaction
--     chạy song song thật sự, không phải xếp hàng chờ nhau.
CREATE TABLE exam_seats (
    exam_seat_id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_seat_exam_session_id   BIGINT      NOT NULL REFERENCES exam_sessions (exam_session_id),
    exam_seat_number            SMALLINT    NOT NULL,
    exam_seat_status            VARCHAR(20) NOT NULL DEFAULT 'available'
                                     CHECK (exam_seat_status IN ('available','held','booked')),
    exam_seat_held_until        TIMESTAMPTZ, -- TTL giữ chỗ tạm (VD 15 phút) trong lúc điền form/thanh toán
    exam_seat_is_deleted        BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_seat_deleted_at        TIMESTAMPTZ,
    exam_seat_created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_seat_updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3.4 exam_registrations: phiếu đăng ký thi = thí sinh + ca thi + ghế đã giữ
CREATE TABLE exam_registrations (
    exam_registration_id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_registration_candidate_id      BIGINT      NOT NULL REFERENCES candidates (candidate_id),
    exam_registration_exam_session_id   BIGINT      NOT NULL REFERENCES exam_sessions (exam_session_id),
    exam_registration_exam_seat_id      BIGINT      NOT NULL REFERENCES exam_seats (exam_seat_id),
    exam_registration_status            VARCHAR(20) NOT NULL DEFAULT 'pending_payment'
                                             CHECK (exam_registration_status IN ('pending_payment','confirmed','cancelled')),
    exam_registration_registered_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_registration_is_deleted        BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_registration_deleted_at        TIMESTAMPTZ,
    exam_registration_created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_registration_updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================================
-- 4. THANH TOÁN, KẾT QUẢ, PHÚC KHẢO
-- =====================================================================

-- 4.1 payments: lệ phí thi, tách khỏi exam_registrations vì 1 phiếu đăng ký
--     có thể phát sinh nhiều lần thử thanh toán (thất bại/retry)
CREATE TABLE payments (
    payment_id                     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_exam_registration_id   BIGINT       NOT NULL REFERENCES exam_registrations (exam_registration_id),
    payment_amount                 NUMERIC(12,0) NOT NULL CHECK (payment_amount >= 0),
    payment_method                 VARCHAR(20)  NOT NULL
                                        CHECK (payment_method IN ('vnpay','momo','zalopay','bank_transfer','cash')),
    payment_transaction_ref        VARCHAR(100),
    payment_status                 VARCHAR(20)  NOT NULL DEFAULT 'pending'
                                        CHECK (payment_status IN ('pending','success','failed','refunded')),
    payment_paid_at                TIMESTAMPTZ,
    payment_is_deleted             BOOLEAN      NOT NULL DEFAULT FALSE,
    payment_deleted_at             TIMESTAMPTZ,
    payment_created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    payment_updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 4.2 exam_results: điểm thi (1-1 với exam_registrations)
CREATE TABLE exam_results (
    exam_result_id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_result_exam_registration_id BIGINT        NOT NULL REFERENCES exam_registrations (exam_registration_id),
    exam_result_listening_score      NUMERIC(5,1)  CHECK (exam_result_listening_score BETWEEN 0 AND 100),
    exam_result_reading_score        NUMERIC(5,1)  CHECK (exam_result_reading_score BETWEEN 0 AND 100),
    exam_result_writing_score        NUMERIC(5,1)  CHECK (exam_result_writing_score BETWEEN 0 AND 100),
    exam_result_total_score          NUMERIC(6,1),
    exam_result_status               VARCHAR(10)   CHECK (exam_result_status IN ('pass','fail')),
    exam_result_published_at         TIMESTAMPTZ,
    exam_result_is_deleted           BOOLEAN       NOT NULL DEFAULT FALSE,
    exam_result_deleted_at           TIMESTAMPTZ,
    exam_result_created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    exam_result_updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- 4.3 exam_recheck_requests: đơn phúc khảo điểm thi
CREATE TABLE exam_recheck_requests (
    exam_recheck_id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exam_recheck_exam_result_id          BIGINT      NOT NULL REFERENCES exam_results (exam_result_id),
    exam_recheck_reason                  TEXT        NOT NULL,
    exam_recheck_status                  VARCHAR(20) NOT NULL DEFAULT 'submitted'
                                              CHECK (exam_recheck_status IN ('submitted','in_review','completed','rejected')),
    exam_recheck_reviewed_by_account_id  BIGINT      REFERENCES accounts (account_id),
    exam_recheck_reviewed_at             TIMESTAMPTZ,
    exam_recheck_new_total_score         NUMERIC(6,1),
    exam_recheck_is_deleted              BOOLEAN     NOT NULL DEFAULT FALSE,
    exam_recheck_deleted_at              TIMESTAMPTZ,
    exam_recheck_created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    exam_recheck_updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================================
-- 5. CHỨNG CHỈ
-- =====================================================================

-- 5.1 certificates: chứng chỉ cấp cho thí sinh đậu (1-1 với exam_results)
CREATE TABLE certificates (
    certificate_id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    certificate_exam_result_id      BIGINT       NOT NULL REFERENCES exam_results (exam_result_id),
    certificate_no                  VARCHAR(30)  NOT NULL,
    certificate_issue_date          DATE         NOT NULL,
    certificate_verification_code   VARCHAR(64)  NOT NULL, -- mã QR tra cứu công khai chống giả mạo
    certificate_status              VARCHAR(20)  NOT NULL DEFAULT 'issued'
                                         CHECK (certificate_status IN ('issued','revoked')),
    certificate_is_deleted          BOOLEAN      NOT NULL DEFAULT FALSE,
    certificate_deleted_at          TIMESTAMPTZ,
    certificate_created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    certificate_updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 5.2 certificate_deliveries: đăng ký chuyển phát chứng chỉ tận nhà
CREATE TABLE certificate_deliveries (
    certificate_delivery_id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    certificate_delivery_certificate_id  BIGINT      NOT NULL REFERENCES certificates (certificate_id),
    certificate_delivery_ward_id         BIGINT      REFERENCES wards (ward_id),
    certificate_delivery_address_detail  VARCHAR(255) NOT NULL,
    certificate_delivery_courier         VARCHAR(50),
    certificate_delivery_tracking_no     VARCHAR(50),
    certificate_delivery_status          VARCHAR(20) NOT NULL DEFAULT 'requested'
                                              CHECK (certificate_delivery_status IN ('requested','shipped','delivered','failed')),
    certificate_delivery_is_deleted      BOOLEAN     NOT NULL DEFAULT FALSE,
    certificate_delivery_deleted_at      TIMESTAMPTZ,
    certificate_delivery_created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    certificate_delivery_updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================================
-- 6. BẢNG PARTITION THEO NGÀY (time-series, ghi nhiều, không cần unique
--    constraint xuyên bảng -> ứng viên lý tưởng để partition)
-- =====================================================================

-- 6.1 notifications: thông báo đa kênh (email/SMS/Zalo), partition THEO THÁNG
--     theo notification_created_at. Không dùng soft-delete kiểu ẩn "xoá mềm"
--     bằng is_deleted riêng cột status vì bản chất là candidate "đánh dấu đã đọc/ẩn"
--     -> vẫn giữ is_deleted để đồng bộ quy ước toàn hệ thống.
CREATE TABLE notifications (
    notification_id         BIGINT GENERATED ALWAYS AS IDENTITY,
    notification_account_id BIGINT      NOT NULL REFERENCES accounts (account_id),
    notification_channel    VARCHAR(10) NOT NULL CHECK (notification_channel IN ('email','sms','zalo','push')),
    notification_content    TEXT        NOT NULL,
    notification_status     VARCHAR(10) NOT NULL DEFAULT 'pending'
                                 CHECK (notification_status IN ('pending','sent','failed')),
    notification_is_deleted BOOLEAN     NOT NULL DEFAULT FALSE,
    notification_deleted_at TIMESTAMPTZ,
    notification_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    notification_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (notification_id, notification_created_at)
) PARTITION BY RANGE (notification_created_at);

-- 6.2 audit_logs: nhật ký thao tác toàn hệ thống (ai duyệt hồ sơ, ai sửa điểm...).
--     Partition THEO NGÀY (daily) vì đây là bảng có khả năng tăng trưởng nhanh
--     nhất khi traffic lớn (mỗi request ghi kèm 1 dòng audit) -> partition nhỏ,
--     drop/archive partition cũ dễ dàng mà không khoá bảng lớn.
--     LƯU Ý: bảng này KHÔNG áp dụng soft-delete (không có is_deleted/deleted_at)
--     -> đây là ngoại lệ có chủ đích: nhật ký audit phải bất biến (immutable),
--     nếu cho phép "xoá mềm" chính audit log thì mất ý nghĩa truy vết.
CREATE TABLE audit_logs (
    audit_log_id                BIGINT GENERATED ALWAYS AS IDENTITY,
    audit_log_actor_account_id  BIGINT REFERENCES accounts (account_id),
    audit_log_action            VARCHAR(50) NOT NULL,
    audit_log_entity_table      VARCHAR(50) NOT NULL,
    audit_log_entity_id         BIGINT      NOT NULL,
    audit_log_old_value         JSONB,
    audit_log_new_value         JSONB,
    audit_log_created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (audit_log_id, audit_log_created_at)
) PARTITION BY RANGE (audit_log_created_at);
