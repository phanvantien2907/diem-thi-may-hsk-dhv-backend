package certificate

import (
	"time"

	"github.com/hskvinh/backend/internal/certificate/enum"
)

// CertificateEntity mirrors the certificates table 1:1.
type CertificateEntity struct {
	CertificateID               int64
	CertificateExamResultID     int64
	CertificateNo               string
	CertificateIssueDate        time.Time
	CertificateVerificationCode string
	CertificateStatus           enum.CertificateStatus
	CertificateIsDeleted        bool
	CertificateDeletedAt        *time.Time
	CertificateCreatedAt        time.Time
	CertificateUpdatedAt        time.Time
}

// CertificateDeliveryEntity mirrors the certificate_deliveries table 1:1.
type CertificateDeliveryEntity struct {
	CertificateDeliveryID            int64
	CertificateDeliveryCertificateID int64
	CertificateDeliveryWardID        *int64
	CertificateDeliveryAddressDetail string
	CertificateDeliveryCourier       *string
	CertificateDeliveryTrackingNo    *string
	CertificateDeliveryStatus        enum.DeliveryStatus
	CertificateDeliveryIsDeleted     bool
	CertificateDeliveryDeletedAt     *time.Time
	CertificateDeliveryCreatedAt     time.Time
	CertificateDeliveryUpdatedAt     time.Time
}
