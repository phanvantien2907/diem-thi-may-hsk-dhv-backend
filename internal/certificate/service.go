package certificate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hskvinh/backend/internal/certificate/dto/request"
	"github.com/hskvinh/backend/internal/certificate/dto/response"
	"github.com/hskvinh/backend/internal/certificate/enum"
)

// Service handles certificate issuance and delivery.
type Service struct{ db *sql.DB }

// NewService creates a certificate Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateCertificate issues a certificate and returns its public DTO.
func (s *Service) CreateCertificate(ctx context.Context, req request.IssueCertificateRequestDTO) (response.CertificateResponseDTO, error) {
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("invalid issue_date: %w", err)
	}
	var e CertificateEntity
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO certificates
		       (certificate_exam_result_id, certificate_no,
		        certificate_issue_date, certificate_verification_code, certificate_status)
		VALUES ($1,$2,$3,$4,'issued')
		RETURNING certificate_id, certificate_exam_result_id, certificate_no,
		          certificate_issue_date, certificate_verification_code,
		          certificate_status, certificate_is_deleted, certificate_deleted_at,
		          certificate_created_at, certificate_updated_at`,
		req.ExamResultID, req.CertificateNo, issueDate, req.VerificationCode,
	).Scan(
		&e.CertificateID, &e.CertificateExamResultID, &e.CertificateNo,
		&e.CertificateIssueDate, &e.CertificateVerificationCode,
		&e.CertificateStatus, &e.CertificateIsDeleted, &e.CertificateDeletedAt,
		&e.CertificateCreatedAt, &e.CertificateUpdatedAt,
	)
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: create: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// GetCertificateByResult returns the certificate DTO for a given result ID.
func (s *Service) GetCertificateByResult(ctx context.Context, resultID int64) (response.CertificateResponseDTO, error) {
	var e CertificateEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT certificate_id, certificate_exam_result_id, certificate_no,
		       certificate_issue_date, certificate_verification_code,
		       certificate_status, certificate_is_deleted, certificate_deleted_at,
		       certificate_created_at, certificate_updated_at
		FROM certificates
		WHERE certificate_exam_result_id=$1 AND certificate_is_deleted=FALSE`, resultID,
	).Scan(
		&e.CertificateID, &e.CertificateExamResultID, &e.CertificateNo,
		&e.CertificateIssueDate, &e.CertificateVerificationCode,
		&e.CertificateStatus, &e.CertificateIsDeleted, &e.CertificateDeletedAt,
		&e.CertificateCreatedAt, &e.CertificateUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CertificateResponseDTO{}, errors.New("certificate not found")
	}
	if err != nil {
		return response.CertificateResponseDTO{}, fmt.Errorf("certificate: get: %w", err)
	}
	return ToCertificateResponseDTO(e), nil
}

// CreateDelivery creates a delivery request and returns its public DTO.
func (s *Service) CreateDelivery(ctx context.Context, certID int64, req request.CreateDeliveryRequestDTO) (response.DeliveryResponseDTO, error) {
	var e CertificateDeliveryEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO certificate_deliveries
		       (certificate_delivery_certificate_id, certificate_delivery_ward_id,
		        certificate_delivery_address_detail, certificate_delivery_courier, certificate_delivery_status)
		VALUES ($1,$2,$3,NULLIF($4,''),'requested')
		RETURNING certificate_delivery_id, certificate_delivery_certificate_id,
		          certificate_delivery_ward_id, certificate_delivery_address_detail,
		          certificate_delivery_courier, certificate_delivery_tracking_no,
		          certificate_delivery_status, certificate_delivery_is_deleted, certificate_delivery_deleted_at,
		          certificate_delivery_created_at, certificate_delivery_updated_at`,
		certID, req.WardID, req.AddressDetail, req.Courier,
	).Scan(
		&e.CertificateDeliveryID, &e.CertificateDeliveryCertificateID,
		&e.CertificateDeliveryWardID, &e.CertificateDeliveryAddressDetail,
		&e.CertificateDeliveryCourier, &e.CertificateDeliveryTrackingNo,
		&e.CertificateDeliveryStatus, &e.CertificateDeliveryIsDeleted, &e.CertificateDeliveryDeletedAt,
		&e.CertificateDeliveryCreatedAt, &e.CertificateDeliveryUpdatedAt,
	)
	if err != nil {
		return response.DeliveryResponseDTO{}, fmt.Errorf("certificate: create delivery: %w", err)
	}
	return ToDeliveryResponseDTO(e), nil
}

// UpdateDeliveryStatus updates status + tracking number.
func (s *Service) UpdateDeliveryStatus(ctx context.Context, deliveryID int64, status enum.DeliveryStatus, trackingNo string) error {
	if !status.Valid() {
		return fmt.Errorf("invalid delivery status: %s", status)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE certificate_deliveries
		SET certificate_delivery_status      = $1,
		    certificate_delivery_tracking_no  = NULLIF($2,''),
		    certificate_delivery_updated_at   = NOW()
		WHERE certificate_delivery_id=$3 AND certificate_delivery_is_deleted=FALSE`,
		status, trackingNo, deliveryID,
	)
	if err != nil {
		return fmt.Errorf("certificate: update delivery: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("delivery not found")
	}
	return nil
}
