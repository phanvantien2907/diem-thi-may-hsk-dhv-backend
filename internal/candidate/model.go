package candidate

import (
	"time"

	"github.com/hskvinh/backend/internal/candidate/enum"
)

// CandidateEntity mirrors the candidates table 1:1.
type CandidateEntity struct {
	CandidateID                int64
	CandidateAccountID         int64
	CandidateFullName          string
	CandidateFullNameCN        *string
	CandidateDOB               time.Time
	CandidateGender            enum.CandidateGender
	CandidateEthnicity         *string
	CandidateReligion          *string
	CandidateBirthplace        *string
	CandidateNationality       string
	CandidateMotherTongue      *string
	CandidateChineseStudyYears *int16
	CandidateIsDeleted         bool
	CandidateDeletedAt         *time.Time
	CandidateCreatedAt         time.Time
	CandidateUpdatedAt         time.Time
}

// CandidateDocumentEntity mirrors the candidate_documents table 1:1.
type CandidateDocumentEntity struct {
	CandidateDocumentID                 int64
	CandidateDocumentCandidateID        int64
	CandidateDocumentType               enum.DocumentType
	CandidateDocumentNumber             string
	CandidateDocumentIssueDate          time.Time
	CandidateDocumentIssuePlace         string
	CandidateDocumentFrontImageURL      string
	CandidateDocumentBackImageURL       *string
	CandidateDocumentPortraitImageURL   string
	CandidateDocumentVerificationStatus enum.DocumentVerificationStatus
	CandidateDocumentWardID             *int64
	CandidateDocumentAddressDetail      *string
	CandidateDocumentIsDeleted          bool
	CandidateDocumentDeletedAt          *time.Time
	CandidateDocumentCreatedAt          time.Time
	CandidateDocumentUpdatedAt          time.Time
}

// ── Internal service inputs ───────────────────────────────────────────────────

// CreateProfileInput carries validated data from CreateProfileRequestDTO.
type CreateProfileInput struct {
	AccountID         int64
	FullName          string
	FullNameCN        string
	DOB               time.Time
	Gender            enum.CandidateGender
	Ethnicity         string
	Religion          string
	Birthplace        string
	Nationality       string
	MotherTongue      string
	ChineseStudyYears *int16
}

// UpdateProfileInput carries validated data from UpdateProfileRequestDTO.
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

// UploadDocumentInput carries validated data from UploadDocumentRequestDTO.
type UploadDocumentInput struct {
	CandidateID      int64
	DocType          enum.DocumentType
	DocNumber        string
	IssueDate        time.Time
	IssuePlace       string
	FrontImageURL    string
	BackImageURL     string
	PortraitImageURL string
	WardID           *int64
	AddressDetail    string
}
