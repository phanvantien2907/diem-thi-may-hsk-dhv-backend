package response

import "time"

// DeliveryResponseDTO is the public delivery representation.
type DeliveryResponseDTO struct {
	ID            int64     `json:"id"`
	CertificateID int64     `json:"certificate_id"`
	WardID        *int64    `json:"ward_id,omitempty"`
	AddressDetail string    `json:"address_detail"`
	Courier       *string   `json:"courier,omitempty"`
	TrackingNo    *string   `json:"tracking_no,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
