package response

import "time"

// CandidateResponseDTO is the public candidate profile representation.
type CandidateResponseDTO struct {
	ID                int64     `json:"id"`
	AccountID         int64     `json:"account_id"`
	FullName          string    `json:"full_name"`
	FullNameCN        *string   `json:"full_name_cn,omitempty"`
	DOB               time.Time `json:"dob"`
	Gender            string    `json:"gender"`
	Ethnicity         *string   `json:"ethnicity,omitempty"`
	Religion          *string   `json:"religion,omitempty"`
	Birthplace        *string   `json:"birthplace,omitempty"`
	Nationality       string    `json:"nationality"`
	MotherTongue      *string   `json:"mother_tongue,omitempty"`
	ChineseStudyYears *int16    `json:"chinese_study_years,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

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
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
