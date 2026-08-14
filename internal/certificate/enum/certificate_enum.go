package enum

// CertificateStatus enumerates certificate states.
type CertificateStatus string

const (
	CertificateIssued  CertificateStatus = "issued"
	CertificateRevoked CertificateStatus = "revoked"
)

// Valid reports whether s is a recognised CertificateStatus.
func (s CertificateStatus) Valid() bool {
	return s == CertificateIssued || s == CertificateRevoked
}

// String returns the string representation.
func (s CertificateStatus) String() string { return string(s) }

// DeliveryStatus enumerates certificate delivery states.
type DeliveryStatus string

const (
	DeliveryRequested DeliveryStatus = "requested"
	DeliveryShipped   DeliveryStatus = "shipped"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

// Valid reports whether s is a recognised DeliveryStatus.
func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryRequested, DeliveryShipped, DeliveryDelivered, DeliveryFailed:
		return true
	}
	return false
}

// String returns the string representation.
func (s DeliveryStatus) String() string { return string(s) }
