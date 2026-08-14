package certificate

import "time"

// ── Request DTOs ──────────────────────────────────────────────────────────────

// IssueCertificateRequestDTO is the admin payload for POST /admin/certificates.
type IssueCertificateRequestDTO struct {
	ExamResultID     int64  `json:"exam_result_id"     validate:"required,min=1"`
	CertificateNo    string `json:"certificate_no"     validate:"required,max=30"`
	IssueDate        string `json:"issue_date"         validate:"required"`        // YYYY-MM-DD
	VerificationCode string `json:"verification_code"  validate:"required,max=64"`
}

// CreateDeliveryRequestDTO is the payload for POST /certificates/:id/deliveries.
type CreateDeliveryRequestDTO struct {
	WardID        *int64 `json:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail string `json:"address_detail" validate:"required,max=255"`
	Courier       string `json:"courier"        validate:"omitempty,max=50"`
}

// ── Response DTOs ─────────────────────────────────────────────────────────────

// CertificateResponseDTO is the public certificate representation.
type CertificateResponseDTO struct {
	ID               int64     `json:"id"`
	ExamResultID     int64     `json:"exam_result_id"`
	CertificateNo    string    `json:"certificate_no"`
	IssueDate        time.Time `json:"issue_date"`
	VerificationCode string    `json:"verification_code"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}

// DeliveryResponseDTO is the public delivery representation.
type DeliveryResponseDTO struct {
	ID            int64      `json:"id"`
	CertificateID int64      `json:"certificate_id"`
	WardID        *int64     `json:"ward_id,omitempty"`
	AddressDetail string     `json:"address_detail"`
	Courier       *string    `json:"courier,omitempty"`
	TrackingNo    *string    `json:"tracking_no,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
