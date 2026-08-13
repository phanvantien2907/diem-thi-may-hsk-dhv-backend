package certificate

import "time"

// ── Certificate ───────────────────────────────────────────────────────────

// CertificateStatus enumerates certificate states.
type CertificateStatus string

const (
	CertificateIssued  CertificateStatus = "issued"
	CertificateRevoked CertificateStatus = "revoked"
)

// Certificate maps to the certificates table.
type Certificate struct {
	CertificateID               int64             `json:"certificate_id"`
	CertificateExamResultID     int64             `json:"certificate_exam_result_id"`
	CertificateNo               string            `json:"certificate_no"`
	CertificateIssueDate        time.Time         `json:"certificate_issue_date"`
	CertificateVerificationCode string            `json:"certificate_verification_code"`
	CertificateStatus           CertificateStatus `json:"certificate_status"`
	CertificateIsDeleted        bool              `json:"certificate_is_deleted"`
	CertificateDeletedAt        *time.Time        `json:"certificate_deleted_at,omitempty"`
	CertificateCreatedAt        time.Time         `json:"certificate_created_at"`
	CertificateUpdatedAt        time.Time         `json:"certificate_updated_at"`
}

// ── CertificateDelivery ───────────────────────────────────────────────────

// DeliveryStatus enumerates certificate delivery states.
type DeliveryStatus string

const (
	DeliveryRequested DeliveryStatus = "requested"
	DeliveryShipped   DeliveryStatus = "shipped"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

// CertificateDelivery maps to the certificate_deliveries table.
type CertificateDelivery struct {
	CertificateDeliveryID            int64          `json:"certificate_delivery_id"`
	CertificateDeliveryCertificateID int64          `json:"certificate_delivery_certificate_id"`
	CertificateDeliveryWardID        *int64         `json:"certificate_delivery_ward_id,omitempty"`
	CertificateDeliveryAddressDetail string         `json:"certificate_delivery_address_detail"`
	CertificateDeliveryCourier       *string        `json:"certificate_delivery_courier,omitempty"`
	CertificateDeliveryTrackingNo    *string        `json:"certificate_delivery_tracking_no,omitempty"`
	CertificateDeliveryStatus        DeliveryStatus `json:"certificate_delivery_status"`
	CertificateDeliveryIsDeleted     bool           `json:"certificate_delivery_is_deleted"`
	CertificateDeliveryDeletedAt     *time.Time     `json:"certificate_delivery_deleted_at,omitempty"`
	CertificateDeliveryCreatedAt     time.Time      `json:"certificate_delivery_created_at"`
	CertificateDeliveryUpdatedAt     time.Time      `json:"certificate_delivery_updated_at"`
}

// ── DTOs ──────────────────────────────────────────────────────────────────

// CreateCertificateInput holds data for issuing a certificate.
type CreateCertificateInput struct {
	ResultID         int64
	CertificateNo    string
	IssueDate        time.Time
	VerificationCode string
}

// CreateDeliveryInput holds data for requesting certificate delivery.
type CreateDeliveryInput struct {
	CertificateID int64
	WardID        *int64
	AddressDetail string
	Courier       string
}
