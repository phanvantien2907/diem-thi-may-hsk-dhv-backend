package certificate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/certificate/dto/request"
	"github.com/hskvinh/backend/internal/certificate/dto/response"
	"github.com/hskvinh/backend/internal/certificate/entities"
	"github.com/hskvinh/backend/internal/certificate/enum"
	"github.com/hskvinh/backend/internal/utils"
)

// Service handles certificate issuance and delivery.
type Service struct {
	db            *sql.DB
	auditSvc      audit.Service
	webhookSecret string // HMAC-SHA256 key for shipping webhook verification
}

// NewService creates a certificate Service.
func NewService(db *sql.DB, auditSvc audit.Service, webhookSecret string) *Service {
	return &Service{db: db, auditSvc: auditSvc, webhookSecret: webhookSecret}
}

// ─── Shared helpers ────────────────────────────────────────────────────────────

// scanCert scans the 7-column projection used in most SELECT/RETURNING queries.
func scanCert(row *sql.Row, e *entities.CertificateEntity) error {
	return row.Scan(
		&e.CertificateID, &e.CertificateExamResultID, &e.CertificateNo,
		&e.CertificateIssueDate, &e.CertificateVerificationCode,
		&e.CertificateStatus, &e.CertificateCreatedAt,
	)
}

// scanDelivery scans the full delivery projection used in SELECT/RETURNING queries.
func scanDelivery(row *sql.Row, e *entities.CertificateDeliveryEntity) error {
	return row.Scan(
		&e.CertificateDeliveryID, &e.CertificateDeliveryCertificateID,
		&e.CertificateDeliveryWardID, &e.CertificateDeliveryAddressDetail,
		&e.CertificateDeliveryCourier, &e.CertificateDeliveryTrackingNo,
		&e.CertificateDeliveryStatus,
		&e.CertificateDeliveryCreatedAt, &e.CertificateDeliveryUpdatedAt,
	)
}

// ─── CreateCertificate ─────────────────────────────────────────────────────────

// CreateCertificate issues a certificate and returns its public DTO.
func (s *Service) CreateCertificate(ctx context.Context, req request.IssueCertificateRequestDTO, actorID int64) (response.CertificateResponseDTO, error) {
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("ngày cấp không hợp lệ: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CertificateEntity
	err = scanCert(tx.QueryRowContext(ctx, `
		INSERT INTO certificates
		       (certificate_exam_result_id, certificate_no,
		        certificate_issue_date, certificate_verification_code, certificate_status)
		VALUES ($1,$2,$3,$4,'issued')
		RETURNING certificate_id, certificate_exam_result_id, certificate_no,
		          certificate_issue_date, certificate_verification_code,
		          certificate_status, certificate_created_at`,
		req.ExamResultID, req.CertificateNo, issueDate, req.VerificationCode,
	), &e)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: create: %w", err)
	}

	// write log
	newVal, _ := json.Marshal(map[string]any{"certificate_no": e.CertificateNo, "result_id": e.CertificateExamResultID})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityCertificate.String(),
		EntityID:       e.CertificateID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: commit: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// ─── GetCertificateByResult ────────────────────────────────────────────────────

// GetCertificateByResult returns the certificate DTO for a given result ID.
func (s *Service) GetCertificateByResult(ctx context.Context, resultID int64) (response.CertificateResponseDTO, error) {
	var e entities.CertificateEntity
	err := scanCert(s.db.QueryRowContext(ctx, `
		SELECT certificate_id, certificate_exam_result_id, certificate_no,
		       certificate_issue_date, certificate_verification_code,
		       certificate_status, certificate_created_at
		FROM certificates
		WHERE certificate_exam_result_id=$1 AND certificate_is_deleted=FALSE`, resultID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CertificateResponseDTO{}, errors.New("không tìm thấy chứng chỉ")
	}
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: get: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// ─── ListMyCertificates ────────────────────────────────────────────────────────

// ListMyCertificates returns paginated certificates belonging to the authenticated candidate.
func (s *Service) ListMyCertificates(ctx context.Context, accountID int64, page, limit int) ([]response.CertificateResponseDTO, utils.Meta, error) {
	offset := (page - 1) * limit

	// count total
	var total int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM certificates c
		JOIN exam_results er ON er.exam_result_id = c.certificate_exam_result_id
		JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		JOIN candidates ca ON ca.candidate_id = r.exam_registration_candidate_id
		WHERE ca.candidate_account_id = $1 AND c.certificate_is_deleted = FALSE`,
		accountID,
	).Scan(&total); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: count: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.certificate_id, c.certificate_exam_result_id, c.certificate_no,
		       c.certificate_issue_date, c.certificate_verification_code,
		       c.certificate_status, c.certificate_created_at
		FROM certificates c
		JOIN exam_results er ON er.exam_result_id = c.certificate_exam_result_id
		JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		JOIN candidates ca ON ca.candidate_id = r.exam_registration_candidate_id
		WHERE ca.candidate_account_id = $1 AND c.certificate_is_deleted = FALSE
		ORDER BY c.certificate_created_at DESC
		LIMIT $2 OFFSET $3`,
		accountID, limit, offset,
	)
	if err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: list: %w", err)
	}
	defer rows.Close()

	var dtos []response.CertificateResponseDTO
	for rows.Next() {
		var e entities.CertificateEntity
		if err := rows.Scan(
			&e.CertificateID, &e.CertificateExamResultID, &e.CertificateNo,
			&e.CertificateIssueDate, &e.CertificateVerificationCode,
			&e.CertificateStatus, &e.CertificateCreatedAt,
		); err != nil {
			return nil, utils.Meta{}, fmt.Errorf("certificate: scan: %w", err)
		}
		dtos = append(dtos, ToCertificateResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: rows: %w", err)
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))
	meta := utils.Meta{Page: page, PerPage: limit, TotalItems: total, TotalPages: totalPages}
	return dtos, meta, nil
}

// ─── GetMyCertificateByID ──────────────────────────────────────────────────────

// GetMyCertificateByID returns a single certificate verifying the caller owns it.
func (s *Service) GetMyCertificateByID(ctx context.Context, certID, accountID int64) (response.CertificateResponseDTO, error) {
	var e entities.CertificateEntity
	err := scanCert(s.db.QueryRowContext(ctx, `
		SELECT c.certificate_id, c.certificate_exam_result_id, c.certificate_no,
		       c.certificate_issue_date, c.certificate_verification_code,
		       c.certificate_status, c.certificate_created_at
		FROM certificates c
		JOIN exam_results er ON er.exam_result_id = c.certificate_exam_result_id
		JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		JOIN candidates ca ON ca.candidate_id = r.exam_registration_candidate_id
		WHERE c.certificate_id = $1
		  AND ca.candidate_account_id = $2
		  AND c.certificate_is_deleted = FALSE`,
		certID, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CertificateResponseDTO{}, errors.New("không tìm thấy chứng chỉ")
	}
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: get mine: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// ─── RevokeCertificate ────────────────────────────────────────────────────────

// RevokeCertificate marks a certificate as revoked and records the reason in audit log.
func (s *Service) RevokeCertificate(ctx context.Context, certID int64, req request.RevokeCertificateRequestDTO, actorID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE certificates
		SET certificate_status = 'revoked'
		WHERE certificate_id = $1 AND certificate_is_deleted = FALSE AND certificate_status != 'revoked'`,
		certID,
	)
	if err != nil {
		return fmt.Errorf("certificate: revoke: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không tìm thấy chứng chỉ hoặc đã bị thu hồi")
	}

	newVal, _ := json.Marshal(map[string]any{"status": "revoked", "reason": req.Reason})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityCertificate.String(),
		EntityID:       certID,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// ─── ListCertificates (admin) ─────────────────────────────────────────────────

// ListCertificates returns a paginated, filtered list for admin.
func (s *Service) ListCertificates(ctx context.Context, req request.ListCertificatesRequestDTO) ([]response.CertificateResponseDTO, utils.Meta, error) {
	offset := (req.Page - 1) * req.Limit

	// Build dynamic WHERE clauses
	args := []any{}
	where := "c.certificate_is_deleted = FALSE"
	idx := 1

	if req.ExamSessionID != nil {
		args = append(args, *req.ExamSessionID)
		where += fmt.Sprintf(" AND r.exam_registration_exam_session_id = $%d", idx)
		idx++
	}
	if req.Status != "" {
		args = append(args, req.Status)
		where += fmt.Sprintf(" AND c.certificate_status = $%d", idx)
		idx++
	}
	if req.From != "" {
		args = append(args, req.From)
		where += fmt.Sprintf(" AND c.certificate_issue_date >= $%d", idx)
		idx++
	}
	if req.To != "" {
		args = append(args, req.To)
		where += fmt.Sprintf(" AND c.certificate_issue_date <= $%d", idx)
		idx++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM certificates c
		 JOIN exam_results er ON er.exam_result_id = c.certificate_exam_result_id
		 LEFT JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		 WHERE `+where,
		countArgs...,
	).Scan(&total); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: admin count: %w", err)
	}

	args = append(args, req.Limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT c.certificate_id, c.certificate_exam_result_id, c.certificate_no,
		       c.certificate_issue_date, c.certificate_verification_code,
		       c.certificate_status, c.certificate_created_at
		FROM certificates c
		JOIN exam_results er ON er.exam_result_id = c.certificate_exam_result_id
		LEFT JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		WHERE %s
		ORDER BY c.certificate_created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1),
		args...,
	)
	if err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: admin list: %w", err)
	}
	defer rows.Close()

	var dtos []response.CertificateResponseDTO
	for rows.Next() {
		var e entities.CertificateEntity
		if err := rows.Scan(
			&e.CertificateID, &e.CertificateExamResultID, &e.CertificateNo,
			&e.CertificateIssueDate, &e.CertificateVerificationCode,
			&e.CertificateStatus, &e.CertificateCreatedAt,
		); err != nil {
			return nil, utils.Meta{}, fmt.Errorf("certificate: admin scan: %w", err)
		}
		dtos = append(dtos, ToCertificateResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: admin rows: %w", err)
	}

	totalPages := int((total + int64(req.Limit) - 1) / int64(req.Limit))
	meta := utils.Meta{Page: req.Page, PerPage: req.Limit, TotalItems: total, TotalPages: totalPages}
	return dtos, meta, nil
}

// ─── IssueBatch ───────────────────────────────────────────────────────────────

// IssueBatch issues certificates for all passed exam_results in an exam session
// that do not yet have a certificate. Returns the number of certificates created.
func (s *Service) IssueBatch(ctx context.Context, req request.BatchIssueRequestDTO, actorID int64) (int, error) {
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return 0, fmt.Errorf("ngày cấp không hợp lệ: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Fetch all eligible result IDs that don't already have a certificate
	rows, err := tx.QueryContext(ctx, `
		SELECT er.exam_result_id
		FROM exam_results er
		WHERE er.exam_result_exam_session_id = $1
		  AND er.exam_result_is_passed = TRUE
		  AND NOT EXISTS (
		      SELECT 1 FROM certificates c
		      WHERE c.certificate_exam_result_id = er.exam_result_id
		        AND c.certificate_is_deleted = FALSE
		  )`,
		req.ExamSessionID,
	)
	if err != nil {
		return 0, fmt.Errorf("certificate: batch fetch results: %w", err)
	}
	defer rows.Close()

	var resultIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("certificate: batch scan: %w", err)
		}
		resultIDs = append(resultIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("certificate: batch rows: %w", err)
	}

	year := issueDate.Year()
	for _, resultID := range resultIDs {
		certNo := fmt.Sprintf("HSK-%d-%d", year, resultID)
		verCode := fmt.Sprintf("%X", sha256.Sum256([]byte(certNo)))[:16]

		var certID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO certificates
			       (certificate_exam_result_id, certificate_no,
			        certificate_issue_date, certificate_verification_code, certificate_status)
			VALUES ($1,$2,$3,$4,'issued')
			RETURNING certificate_id`,
			resultID, certNo, issueDate, verCode,
		).Scan(&certID); err != nil {
			return 0, fmt.Errorf("certificate: batch insert result %d: %w", resultID, err)
		}

		newVal, _ := json.Marshal(map[string]any{"certificate_no": certNo, "result_id": resultID})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &actorID,
			Action:         auditenum.AuditActionCreate.String(),
			EntityTable:    auditenum.AuditEntityCertificate.String(),
			EntityID:       certID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("certificate: batch commit: %w", err)
	}
	return len(resultIDs), nil
}

// ─── VerifyByCode ─────────────────────────────────────────────────────────────

// VerifyByCode looks up an issued (non-revoked) certificate by its verification code.
func (s *Service) VerifyByCode(ctx context.Context, code string) (response.CertificateResponseDTO, error) {
	var e entities.CertificateEntity
	err := scanCert(s.db.QueryRowContext(ctx, `
		SELECT certificate_id, certificate_exam_result_id, certificate_no,
		       certificate_issue_date, certificate_verification_code,
		       certificate_status, certificate_created_at
		FROM certificates
		WHERE certificate_verification_code = $1
		  AND certificate_status = 'issued'
		  AND certificate_is_deleted = FALSE`,
		code,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CertificateResponseDTO{}, errors.New("không tìm thấy chứng chỉ")
	}
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: verify code: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// ─── VerifyByNoAndDOB ─────────────────────────────────────────────────────────

// VerifyByNoAndDOB looks up a certificate by its number, requiring the candidate's DOB
// to prevent sequential enumeration attacks.
func (s *Service) VerifyByNoAndDOB(ctx context.Context, certNo, dobStr string) (response.CertificateResponseDTO, error) {
	dob, err := time.Parse("2006-01-02", dobStr)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("ngày sinh không hợp lệ: %w", err)
	}

	var e entities.CertificateEntity
	err = scanCert(s.db.QueryRowContext(ctx, `
		SELECT c.certificate_id, c.certificate_exam_result_id, c.certificate_no,
		       c.certificate_issue_date, c.certificate_verification_code,
		       c.certificate_status, c.certificate_created_at
		FROM certificates c
		JOIN exam_results   er ON er.exam_result_id   = c.certificate_exam_result_id
		JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		JOIN candidates     ca ON ca.candidate_id     = r.exam_registration_candidate_id
		WHERE c.certificate_no = $1
		  AND ca.candidate_dob = $2
		  AND c.certificate_status = 'issued'
		  AND c.certificate_is_deleted = FALSE`,
		certNo, dob,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CertificateResponseDTO{}, errors.New("không tìm thấy chứng chỉ hoặc thông tin không khớp")
	}
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: verify no+dob: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// ─── CreateDelivery ───────────────────────────────────────────────────────────

// CreateDelivery creates a delivery request and returns its public DTO.
func (s *Service) CreateDelivery(ctx context.Context, certID int64, req request.CreateDeliveryRequestDTO, actorID int64) (response.DeliveryResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CertificateDeliveryEntity
	err = scanDelivery(tx.QueryRowContext(ctx, `
		INSERT INTO certificate_deliveries
		       (certificate_delivery_certificate_id, certificate_delivery_ward_id,
		        certificate_delivery_address_detail, certificate_delivery_courier, certificate_delivery_status)
		VALUES ($1,$2,$3,NULLIF($4,''),'requested')
		RETURNING certificate_delivery_id, certificate_delivery_certificate_id,
		          certificate_delivery_ward_id, certificate_delivery_address_detail,
		          certificate_delivery_courier, certificate_delivery_tracking_no,
		          certificate_delivery_status,
		          certificate_delivery_created_at, certificate_delivery_updated_at`,
		certID, req.WardID, req.AddressDetail, req.Courier,
	), &e)
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: create delivery: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"certificate_id": certID, "address": req.AddressDetail})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityCertificateDelivery.String(),
		EntityID:       e.CertificateDeliveryID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: commit: %w", err)
	}
	return ToDeliveryResponseDTO(e), nil
}

// ─── GetDeliveryByCertID ──────────────────────────────────────────────────────

// GetDeliveryByCertID returns the delivery record for a given certificate.
func (s *Service) GetDeliveryByCertID(ctx context.Context, certID int64) (response.DeliveryResponseDTO, error) {
	var e entities.CertificateDeliveryEntity
	err := scanDelivery(s.db.QueryRowContext(ctx, `
		SELECT certificate_delivery_id, certificate_delivery_certificate_id,
		       certificate_delivery_ward_id, certificate_delivery_address_detail,
		       certificate_delivery_courier, certificate_delivery_tracking_no,
		       certificate_delivery_status,
		       certificate_delivery_created_at, certificate_delivery_updated_at
		FROM certificate_deliveries
		WHERE certificate_delivery_certificate_id = $1
		  AND certificate_delivery_is_deleted = FALSE
		ORDER BY certificate_delivery_created_at DESC
		LIMIT 1`,
		certID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.DeliveryResponseDTO{}, errors.New("không tìm thấy đơn giao hàng")
	}
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: get delivery: %w", err)
	}
	return ToDeliveryResponseDTO(e), nil
}

// ─── UpdateDeliveryAddress ────────────────────────────────────────────────────

// UpdateDeliveryAddress lets the candidate change address only when status = requested.
func (s *Service) UpdateDeliveryAddress(ctx context.Context, deliveryID, accountID int64, req request.UpdateDeliveryAddressRequestDTO) (response.DeliveryResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CertificateDeliveryEntity
	err = scanDelivery(tx.QueryRowContext(ctx, `
		UPDATE certificate_deliveries d
		SET certificate_delivery_ward_id        = $1,
		    certificate_delivery_address_detail = $2
		FROM certificates c
		JOIN exam_results  er ON er.exam_result_id  = c.certificate_exam_result_id
		JOIN exam_registrations r ON r.exam_registration_id = er.exam_result_exam_registration_id
		JOIN candidates ca ON ca.candidate_id = r.exam_registration_candidate_id
		WHERE d.certificate_delivery_id = $3
		  AND d.certificate_delivery_certificate_id = c.certificate_id
		  AND ca.candidate_account_id = $4
		  AND d.certificate_delivery_status = 'requested'
		  AND d.certificate_delivery_is_deleted = FALSE
		RETURNING d.certificate_delivery_id, d.certificate_delivery_certificate_id,
		          d.certificate_delivery_ward_id, d.certificate_delivery_address_detail,
		          d.certificate_delivery_courier, d.certificate_delivery_tracking_no,
		          d.certificate_delivery_status,
		          d.certificate_delivery_created_at, d.certificate_delivery_updated_at`,
		req.WardID, req.AddressDetail, deliveryID, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.DeliveryResponseDTO{}, errors.New("không tìm thấy đơn hoặc không thể đổi địa chỉ lúc này")
	}
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: update address: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"ward_id": req.WardID, "address_detail": req.AddressDetail})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityCertificateDelivery.String(),
		EntityID:       deliveryID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: commit: %w", err)
	}
	return ToDeliveryResponseDTO(e), nil
}

// ─── ListDeliveries (admin) ───────────────────────────────────────────────────

// ListDeliveries returns a paginated list of deliveries for admin.
func (s *Service) ListDeliveries(ctx context.Context, req request.ListDeliveriesRequestDTO) ([]response.DeliveryResponseDTO, utils.Meta, error) {
	offset := (req.Page - 1) * req.Limit

	where := "certificate_delivery_is_deleted = FALSE"
	args := []any{}
	idx := 1
	if req.Status != "" {
		args = append(args, req.Status)
		where += fmt.Sprintf(" AND certificate_delivery_status = $%d", idx)
		idx++
	}

	countArgs := make([]any, len(args))
	copy(countArgs, args)

	var total int64
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM certificate_deliveries WHERE "+where,
		countArgs...,
	).Scan(&total); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: delivery count: %w", err)
	}

	args = append(args, req.Limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT certificate_delivery_id, certificate_delivery_certificate_id,
		       certificate_delivery_ward_id, certificate_delivery_address_detail,
		       certificate_delivery_courier, certificate_delivery_tracking_no,
		       certificate_delivery_status,
		       certificate_delivery_created_at, certificate_delivery_updated_at
		FROM certificate_deliveries
		WHERE %s
		ORDER BY certificate_delivery_created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1),
		args...,
	)
	if err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: delivery list: %w", err)
	}
	defer rows.Close()

	var dtos []response.DeliveryResponseDTO
	for rows.Next() {
		var e entities.CertificateDeliveryEntity
		if err := rows.Scan(
			&e.CertificateDeliveryID, &e.CertificateDeliveryCertificateID,
			&e.CertificateDeliveryWardID, &e.CertificateDeliveryAddressDetail,
			&e.CertificateDeliveryCourier, &e.CertificateDeliveryTrackingNo,
			&e.CertificateDeliveryStatus,
			&e.CertificateDeliveryCreatedAt, &e.CertificateDeliveryUpdatedAt,
		); err != nil {
			return nil, utils.Meta{}, fmt.Errorf("certificate: delivery scan: %w", err)
		}
		dtos = append(dtos, ToDeliveryResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("certificate: delivery rows: %w", err)
	}

	totalPages := int((total + int64(req.Limit) - 1) / int64(req.Limit))
	meta := utils.Meta{Page: req.Page, PerPage: req.Limit, TotalItems: total, TotalPages: totalPages}
	return dtos, meta, nil
}

// ─── AdminUpdateDelivery ──────────────────────────────────────────────────────

// AdminUpdateDelivery assigns courier + tracking number and marks delivery as shipped.
func (s *Service) AdminUpdateDelivery(ctx context.Context, deliveryID int64, req request.AdminUpdateDeliveryRequestDTO, actorID int64) (response.DeliveryResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CertificateDeliveryEntity
	err = scanDelivery(tx.QueryRowContext(ctx, `
		UPDATE certificate_deliveries
		SET certificate_delivery_courier     = $1,
		    certificate_delivery_tracking_no = $2,
		    certificate_delivery_status      = 'shipped'
		WHERE certificate_delivery_id = $3
		  AND certificate_delivery_is_deleted = FALSE
		RETURNING certificate_delivery_id, certificate_delivery_certificate_id,
		          certificate_delivery_ward_id, certificate_delivery_address_detail,
		          certificate_delivery_courier, certificate_delivery_tracking_no,
		          certificate_delivery_status,
		          certificate_delivery_created_at, certificate_delivery_updated_at`,
		req.Courier, req.TrackingNo, deliveryID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.DeliveryResponseDTO{}, errors.New("không tìm thấy đơn giao hàng")
	}
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: admin update delivery: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"courier": req.Courier, "tracking_no": req.TrackingNo, "status": "shipped"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityCertificateDelivery.String(),
		EntityID:       deliveryID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: commit: %w", err)
	}
	return ToDeliveryResponseDTO(e), nil
}

// ─── UpdateDeliveryStatus ─────────────────────────────────────────────────────

// UpdateDeliveryStatus updates status + tracking number (used by webhook).
func (s *Service) UpdateDeliveryStatus(ctx context.Context, deliveryID int64, status enum.DeliveryStatus, trackingNo string, actorID int64) error {
	if !status.Valid() {
		return fmt.Errorf("trạng thái giao hàng không hợp lệ: %s", status)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("certificate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE certificate_deliveries
		SET certificate_delivery_status     = $1,
		    certificate_delivery_tracking_no = NULLIF($2,'')
		WHERE certificate_delivery_id = $3 AND certificate_delivery_is_deleted = FALSE`,
		status, trackingNo, deliveryID,
	)
	if err != nil {
		return fmt.Errorf("certificate: update delivery: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không tìm thấy đơn giao hàng")
	}

	newVal, _ := json.Marshal(map[string]any{"status": status.String(), "tracking_no": trackingNo})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityCertificateDelivery.String(),
		EntityID:       deliveryID,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// ─── HandleShippingWebhook ────────────────────────────────────────────────────

// HandleShippingWebhook processes an HMAC-verified callback from a courier.
// sig is the raw value of the X-Signature header.
func (s *Service) HandleShippingWebhook(ctx context.Context, courier string, body []byte, sig string) error {
	// Verify HMAC-SHA256 signature
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return errors.New("chữ ký webhook không hợp lệ")
	}

	var req request.ShippingWebhookRequestDTO
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Errorf("webhook: unmarshal: %w", err)
	}

	// Look up delivery by tracking_no
	var deliveryID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT certificate_delivery_id FROM certificate_deliveries
		WHERE certificate_delivery_tracking_no = $1
		  AND certificate_delivery_is_deleted = FALSE`,
		req.TrackingNo,
	).Scan(&deliveryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("webhook: không tìm thấy đơn giao hàng với tracking_no=%s", req.TrackingNo)
		}
		return fmt.Errorf("webhook: query: %w", err)
	}

	sysID := int64(0) // system actor
	return s.UpdateDeliveryStatus(ctx, deliveryID, enum.DeliveryStatus(req.Status), req.TrackingNo, sysID)
}
