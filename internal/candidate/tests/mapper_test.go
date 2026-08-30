package tests

import (
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/candidate/enum"
)

func TestToCandidateResponseDTO(t *testing.T) {
	dob := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	eth := "Kinh"
	nat := "Vietnam"
	studyYrs := int16(2)
	cnName := "Nguyen"

	entity := entities.CandidateEntity{
		CandidateID:                1,
		CandidateAccountID:         2,
		CandidateFullName:          "Nguyen Van A",
		CandidateFullNameCN:        &cnName,
		CandidateDOB:               dob,
		CandidateGender:            enum.GenderMale,
		CandidateEthnicity:         &eth,
		CandidateNationality:       nat,
		CandidateChineseStudyYears: &studyYrs,
	}

	dto := candidate.ToCandidateResponseDTO(entity)

	if dto.ID != 1 {
		t.Errorf("expected ID 1, got %d", dto.ID)
	}
	if dto.FullName != "Nguyen Van A" {
		t.Errorf("expected FullName 'Nguyen Van A', got %s", dto.FullName)
	}
	if dto.Gender != string(enum.GenderMale) {
		t.Errorf("expected Gender 'male', got %s", dto.Gender)
	}
	if dto.Ethnicity == nil || *dto.Ethnicity != "Kinh" {
		t.Errorf("expected Ethnicity 'Kinh', got %v", dto.Ethnicity)
	}
}

func TestToDocumentResponseDTO(t *testing.T) {
	issueDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rejectReason := "Blurry image"

	entity := entities.CandidateDocumentEntity{
		CandidateDocumentID:                 10,
		CandidateDocumentCandidateID:        1,
		CandidateDocumentType:               enum.DocumentTypeCCCD,
		CandidateDocumentNumber:             "012345678900",
		CandidateDocumentIssueDate:          issueDate,
		CandidateDocumentVerificationStatus: enum.DocVerificationRejected,
		CandidateDocumentRejectionReason:    &rejectReason,
	}

	dto := candidate.ToDocumentResponseDTO(entity)

	if dto.ID != 10 {
		t.Errorf("expected ID 10, got %d", dto.ID)
	}
	if dto.DocType != string(enum.DocumentTypeCCCD) {
		t.Errorf("expected DocType 'cccd', got %s", dto.DocType)
	}
	if dto.RejectionReason == nil || *dto.RejectionReason != "Blurry image" {
		t.Errorf("expected RejectionReason 'Blurry image', got %v", dto.RejectionReason)
	}
}
