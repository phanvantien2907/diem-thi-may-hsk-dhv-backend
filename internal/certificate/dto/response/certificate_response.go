package response

import "time"

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
