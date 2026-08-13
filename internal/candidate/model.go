package candidate

import "time"

// ── Candidate ─────────────────────────────────────────────────────────────

// CandidateGender enumerates valid gender values.
type CandidateGender string

const (
	GenderMale   CandidateGender = "male"
	GenderFemale CandidateGender = "female"
	GenderOther  CandidateGender = "other"
)

// Candidate maps to the candidates table (1-1 with accounts).
type Candidate struct {
	CandidateID                int64           `json:"candidate_id"`
	CandidateAccountID         int64           `json:"candidate_account_id"`
	CandidateFullName          string          `json:"candidate_full_name"`
	CandidateFullNameCN        *string         `json:"candidate_full_name_cn,omitempty"`
	CandidateDOB               time.Time       `json:"candidate_dob"`
	CandidateGender            CandidateGender `json:"candidate_gender"`
	CandidateEthnicity         *string         `json:"candidate_ethnicity,omitempty"`
	CandidateReligion          *string         `json:"candidate_religion,omitempty"`
	CandidateBirthplace        *string         `json:"candidate_birthplace,omitempty"`
	CandidateNationality       string          `json:"candidate_nationality"`
	CandidateMotherTongue      *string         `json:"candidate_mother_tongue,omitempty"`
	CandidateChineseStudyYears *int16          `json:"candidate_chinese_study_years,omitempty"`
	CandidateIsDeleted         bool            `json:"candidate_is_deleted"`
	CandidateDeletedAt         *time.Time      `json:"candidate_deleted_at,omitempty"`
	CandidateCreatedAt         time.Time       `json:"candidate_created_at"`
	CandidateUpdatedAt         time.Time       `json:"candidate_updated_at"`
}

// ── CandidateDocument ─────────────────────────────────────────────────────

// DocumentType enumerates identity document kinds.
type DocumentType string

const (
	DocumentTypeCCCD     DocumentType = "cccd"
	DocumentTypePassport DocumentType = "passport"
)

// DocumentVerificationStatus enumerates review outcomes.
type DocumentVerificationStatus string

const (
	DocVerificationPending  DocumentVerificationStatus = "pending"
	DocVerificationVerified DocumentVerificationStatus = "verified"
	DocVerificationRejected DocumentVerificationStatus = "rejected"
)

// CandidateDocument maps to the candidate_documents table.
type CandidateDocument struct {
	CandidateDocumentID                 int64                      `json:"candidate_document_id"`
	CandidateDocumentCandidateID        int64                      `json:"candidate_document_candidate_id"`
	CandidateDocumentType               DocumentType               `json:"candidate_document_type"`
	CandidateDocumentNumber             string                     `json:"candidate_document_number"`
	CandidateDocumentIssueDate          time.Time                  `json:"candidate_document_issue_date"`
	CandidateDocumentIssuePlace         string                     `json:"candidate_document_issue_place"`
	CandidateDocumentFrontImageURL      string                     `json:"candidate_document_front_image_url"`
	CandidateDocumentBackImageURL       *string                    `json:"candidate_document_back_image_url,omitempty"`
	CandidateDocumentPortraitImageURL   string                     `json:"candidate_document_portrait_image_url"`
	CandidateDocumentVerificationStatus DocumentVerificationStatus `json:"candidate_document_verification_status"`
	CandidateDocumentWardID             *int64                     `json:"candidate_document_ward_id,omitempty"`
	CandidateDocumentAddressDetail      *string                    `json:"candidate_document_address_detail,omitempty"`
	CandidateDocumentIsDeleted          bool                       `json:"candidate_document_is_deleted"`
	CandidateDocumentDeletedAt          *time.Time                 `json:"candidate_document_deleted_at,omitempty"`
	CandidateDocumentCreatedAt          time.Time                  `json:"candidate_document_created_at"`
	CandidateDocumentUpdatedAt          time.Time                  `json:"candidate_document_updated_at"`
}

// ── DTO / request shapes ──────────────────────────────────────────────────

// CreateProfileInput holds fields needed to create a candidate profile.
type CreateProfileInput struct {
	AccountID          int64
	FullName           string
	FullNameCN         string
	DOB                time.Time
	Gender             CandidateGender
	Ethnicity          string
	Religion           string
	Birthplace         string
	Nationality        string
	MotherTongue       string
	ChineseStudyYears  *int16
}

// UpdateProfileInput holds mutable candidate fields.
type UpdateProfileInput struct {
	FullName          string
	FullNameCN        string
	Ethnicity         string
	Religion          string
	Birthplace        string
	Nationality       string
	MotherTongue      string
	ChineseStudyYears *int16
}

// UploadDocumentInput holds identity document data for insertion.
type UploadDocumentInput struct {
	CandidateID      int64
	DocType          DocumentType
	DocNumber        string
	IssueDate        time.Time
	IssuePlace       string
	FrontImageURL    string
	BackImageURL     string
	PortraitImageURL string
	WardID           *int64
	AddressDetail    string
}
