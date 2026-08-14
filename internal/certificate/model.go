package certificate

import "time"

// CertificateEntity mirrors the certificates table 1:1.
type CertificateEntity struct {
	CertificateID               int64
	CertificateExamResultID     int64
	CertificateNo               string
	CertificateIssueDate        time.Time
	CertificateVerificationCode string
	CertificateStatus           CertificateStatus
	CertificateIsDeleted        bool
	CertificateDeletedAt        *time.Time
	CertificateCreatedAt        time.Time
	CertificateUpdatedAt        time.Time
}

// CertificateDeliveryEntity mirrors the certificate_deliveries table 1:1.
type CertificateDeliveryEntity struct {
	CertificateDeliveryID             int64
	CertificateDeliveryCertificateID  int64
	CertificateDeliveryWardID         *int64
	CertificateDeliveryAddressDetail  string
	CertificateDeliveryCourier        *string
	CertificateDeliveryTrackingNo     *string
	CertificateDeliveryStatus         DeliveryStatus
	CertificateDeliveryIsDeleted      bool
	CertificateDeliveryDeletedAt      *time.Time
	CertificateDeliveryCreatedAt      time.Time
	CertificateDeliveryUpdatedAt      time.Time
}

// ── Internal service inputs ───────────────────────────────────────────────────

// CreateCertificateInput carries validated data from IssueCertificateRequestDTO.
type CreateCertificateInput struct {
	ResultID         int64
	CertificateNo    string
	IssueDate        time.Time
	VerificationCode string
}

// CreateDeliveryInput carries validated data from CreateDeliveryRequestDTO.
type CreateDeliveryInput struct {
	CertificateID int64
	WardID        *int64
	AddressDetail string
	Courier       string
}
