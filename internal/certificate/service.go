package certificate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles certificate issuance and delivery.
type Service struct{ db *sql.DB }

// NewService creates a certificate Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateCertificate issues a certificate for a passing result.
func (s *Service) CreateCertificate(ctx context.Context, in CreateCertificateInput) (*Certificate, error) {
	var c Certificate
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO certificates
		       (certificate_exam_result_id, certificate_no,
		        certificate_issue_date, certificate_verification_code, certificate_status)
		VALUES ($1,$2,$3,$4,'issued')
		RETURNING certificate_id, certificate_exam_result_id, certificate_no,
		          certificate_issue_date, certificate_verification_code,
		          certificate_status, certificate_is_deleted,
		          certificate_created_at, certificate_updated_at`,
		in.ResultID, in.CertificateNo, in.IssueDate, in.VerificationCode,
	).Scan(
		&c.CertificateID, &c.CertificateExamResultID, &c.CertificateNo,
		&c.CertificateIssueDate, &c.CertificateVerificationCode,
		&c.CertificateStatus, &c.CertificateIsDeleted,
		&c.CertificateCreatedAt, &c.CertificateUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("certificate: create: %w", err)
	}
	return &c, nil
}

// GetCertificateByResult returns the certificate for a given exam result.
func (s *Service) GetCertificateByResult(ctx context.Context, resultID int64) (*Certificate, error) {
	var c Certificate
	err := s.db.QueryRowContext(ctx, `
		SELECT certificate_id, certificate_exam_result_id, certificate_no,
		       certificate_issue_date, certificate_verification_code,
		       certificate_status, certificate_is_deleted,
		       certificate_created_at, certificate_updated_at
		FROM certificates
		WHERE certificate_exam_result_id = $1 AND certificate_is_deleted = FALSE`,
		resultID,
	).Scan(
		&c.CertificateID, &c.CertificateExamResultID, &c.CertificateNo,
		&c.CertificateIssueDate, &c.CertificateVerificationCode,
		&c.CertificateStatus, &c.CertificateIsDeleted,
		&c.CertificateCreatedAt, &c.CertificateUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("certificate not found")
	}
	if err != nil {
		return nil, fmt.Errorf("certificate: get by result: %w", err)
	}
	return &c, nil
}

// CreateDelivery registers a delivery request for a certificate.
func (s *Service) CreateDelivery(ctx context.Context, in CreateDeliveryInput) (*CertificateDelivery, error) {
	var d CertificateDelivery
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO certificate_deliveries
		       (certificate_delivery_certificate_id, certificate_delivery_ward_id,
		        certificate_delivery_address_detail, certificate_delivery_courier,
		        certificate_delivery_status)
		VALUES ($1,$2,$3,$4,'requested')
		RETURNING certificate_delivery_id, certificate_delivery_certificate_id,
		          certificate_delivery_ward_id, certificate_delivery_address_detail,
		          certificate_delivery_courier, certificate_delivery_tracking_no,
		          certificate_delivery_status, certificate_delivery_is_deleted,
		          certificate_delivery_created_at, certificate_delivery_updated_at`,
		in.CertificateID, in.WardID, in.AddressDetail, nullStr(in.Courier),
	).Scan(
		&d.CertificateDeliveryID, &d.CertificateDeliveryCertificateID,
		&d.CertificateDeliveryWardID, &d.CertificateDeliveryAddressDetail,
		&d.CertificateDeliveryCourier, &d.CertificateDeliveryTrackingNo,
		&d.CertificateDeliveryStatus, &d.CertificateDeliveryIsDeleted,
		&d.CertificateDeliveryCreatedAt, &d.CertificateDeliveryUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("certificate: create delivery: %w", err)
	}
	return &d, nil
}

// UpdateDeliveryStatus updates the status and optional tracking number.
func (s *Service) UpdateDeliveryStatus(ctx context.Context, deliveryID int64, status DeliveryStatus, trackingNo string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE certificate_deliveries
		SET certificate_delivery_status      = $1,
		    certificate_delivery_tracking_no = NULLIF($2,''),
		    certificate_delivery_updated_at  = NOW()
		WHERE certificate_delivery_id = $3 AND certificate_delivery_is_deleted = FALSE`,
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

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
