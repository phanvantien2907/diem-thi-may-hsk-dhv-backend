package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/certificate/enum"
)

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
