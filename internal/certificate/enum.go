package certificate

// CertificateStatus enumerates certificate states.
type CertificateStatus string

const (
	CertificateIssued  CertificateStatus = "issued"
	CertificateRevoked CertificateStatus = "revoked"
)

func (s CertificateStatus) Valid() bool {
	return s == CertificateIssued || s == CertificateRevoked
}

// DeliveryStatus enumerates certificate delivery states.
type DeliveryStatus string

const (
	DeliveryRequested DeliveryStatus = "requested"
	DeliveryShipped   DeliveryStatus = "shipped"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryRequested, DeliveryShipped, DeliveryDelivered, DeliveryFailed:
		return true
	}
	return false
}
