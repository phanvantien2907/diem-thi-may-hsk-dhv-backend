package candidate

import "time"

// ── Request DTOs ──────────────────────────────────────────────────────────────

// CreateProfileRequestDTO is the payload for POST /candidates.
type CreateProfileRequestDTO struct {
	FullName          string  `json:"full_name"           validate:"required,max=150"`
	FullNameCN        string  `json:"full_name_cn"        validate:"omitempty,max=150"`
	DOB               string  `json:"dob"                 validate:"required"`       // YYYY-MM-DD
	Gender            string  `json:"gender"              validate:"required,oneof=male female other"`
	Ethnicity         string  `json:"ethnicity"           validate:"omitempty,max=50"`
	Religion          string  `json:"religion"            validate:"omitempty,max=50"`
	Birthplace        string  `json:"birthplace"          validate:"omitempty,max=150"`
	Nationality       string  `json:"nationality"         validate:"omitempty,max=50"`
	MotherTongue      string  `json:"mother_tongue"       validate:"omitempty,max=50"`
	ChineseStudyYears *int16  `json:"chinese_study_years" validate:"omitempty,min=0"`
}

// UpdateProfileRequestDTO is the payload for PUT /candidates/:id.
type UpdateProfileRequestDTO struct {
	FullName          string  `json:"full_name"           validate:"omitempty,max=150"`
	FullNameCN        string  `json:"full_name_cn"        validate:"omitempty,max=150"`
	Ethnicity         string  `json:"ethnicity"           validate:"omitempty,max=50"`
	Religion          string  `json:"religion"            validate:"omitempty,max=50"`
	Birthplace        string  `json:"birthplace"          validate:"omitempty,max=150"`
	Nationality       string  `json:"nationality"         validate:"omitempty,max=50"`
	MotherTongue      string  `json:"mother_tongue"       validate:"omitempty,max=50"`
	ChineseStudyYears *int16  `json:"chinese_study_years" validate:"omitempty,min=0"`
}

// UploadDocumentRequestDTO is the payload for POST /candidates/:id/documents.
type UploadDocumentRequestDTO struct {
	DocType          string `json:"doc_type"           validate:"required,oneof=cccd passport"`
	DocNumber        string `json:"doc_number"         validate:"required,max=30"`
	IssueDate        string `json:"issue_date"         validate:"required"`         // YYYY-MM-DD
	IssuePlace       string `json:"issue_place"        validate:"required,max=150"`
	FrontImageURL    string `json:"front_image_url"    validate:"required,max=255"`
	BackImageURL     string `json:"back_image_url"     validate:"omitempty,max=255"`
	PortraitImageURL string `json:"portrait_image_url" validate:"required,max=255"`
	WardID           *int64 `json:"ward_id"            validate:"omitempty,min=1"`
	AddressDetail    string `json:"address_detail"     validate:"omitempty,max=255"`
}

// ── Response DTOs ─────────────────────────────────────────────────────────────

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
