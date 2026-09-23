package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/candidate/enum"
)

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
	CandidateDocumentRejectionReason    *string
	CandidateDocumentWardID             *int64
	CandidateDocumentProvinceID         *int64
	CandidateDocumentAddressDetail      *string
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
