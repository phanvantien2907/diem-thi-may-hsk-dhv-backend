package response

import "time"

// DocumentResponseDTO is the public document representation.
type DocumentResponseDTO struct {
	ID                 int64     `json:"id"`
	CandidateID        int64     `json:"candidate_id"`
	DocType            string    `json:"doc_type"`
	DocNumber          string    `json:"doc_number"`
	IssueDate          time.Time `json:"issue_date"`
	IssuePlace         string    `json:"issue_place"`
	FrontImageURL      string    `json:"front_image_url"`
	BackImageURL       *string   `json:"back_image_url,omitempty"`
	PortraitImageURL   string    `json:"portrait_image_url"`
	VerificationStatus string    `json:"verification_status"`
	RejectionReason    *string   `json:"rejection_reason,omitempty"`
	WardID             *int64    `json:"ward_id,omitempty"`
	ProvinceID         *int64    `json:"province_id,omitempty"`
	AddressDetail      *string   `json:"address_detail,omitempty"`
}
