package request

// IssueCertificateRequestDTO is the admin payload for POST /admin/certificates.
type IssueCertificateRequestDTO struct {
	ExamResultID     int64  `json:"exam_result_id"    validate:"required,min=1"`
	CertificateNo    string `json:"certificate_no"    validate:"required,max=30"`
	IssueDate        string `json:"issue_date"        validate:"required"`         // YYYY-MM-DD
	VerificationCode string `json:"verification_code" validate:"required,max=64"`
}

// CreateDeliveryRequestDTO is the payload for POST /certificates/:id/deliveries.
type CreateDeliveryRequestDTO struct {
	WardID        *int64 `json:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail string `json:"address_detail" validate:"required,max=255"`
	Courier       string `json:"courier"        validate:"omitempty,max=50"`
}
