package response

import "time"

// AdminDocumentResponseDTO is the complete latest identity document view for admins.
type AdminDocumentResponseDTO struct {
	ID                 int64     `json:"id"`
	CandidateID        int64     `json:"candidate_id"`
	DocType            string    `json:"doc_type"`
	DocNumber          string    `json:"doc_number"`
	IssueDate          time.Time `json:"issue_date"`
	IssuePlace         string    `json:"issue_place"`
	FrontImageURL      string    `json:"front_image_url"`
	BackImageURL       *string   `json:"back_image_url"`
	PortraitImageURL   string    `json:"portrait_image_url"`
	HasFrontImage      bool      `json:"has_front_image"`
	HasBackImage       bool      `json:"has_back_image"`
	HasPortraitImage   bool      `json:"has_portrait_image"`
	PiiLocked          bool      `json:"pii_locked"`
	VerificationStatus string    `json:"verification_status"`
	RejectionReason    *string   `json:"rejection_reason"`
	WardID             *int64    `json:"ward_id"`
	ProvinceID         *int64    `json:"province_id"`
	AddressDetail      *string   `json:"address_detail"`
}
