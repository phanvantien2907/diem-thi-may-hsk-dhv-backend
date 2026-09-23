package candidate

import (
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/entities"
)

// ToCandidateResponseDTO maps a CandidateEntity to its public DTO.
func ToCandidateResponseDTO(e entities.CandidateEntity) response.CandidateResponseDTO {
	return response.CandidateResponseDTO{
		ID:                e.CandidateID,
		AccountID:         e.CandidateAccountID,
		FullName:          e.CandidateFullName,
		FullNameCN:        e.CandidateFullNameCN,
		DOB:               e.CandidateDOB,
		Gender:            string(e.CandidateGender),
		Ethnicity:         e.CandidateEthnicity,
		Religion:          e.CandidateReligion,
		Birthplace:        e.CandidateBirthplace,
		Nationality:       e.CandidateNationality,
		MotherTongue:      e.CandidateMotherTongue,
		ChineseStudyYears: e.CandidateChineseStudyYears,
	}
}

// ToAdminCandidateResponseDTO maps a CandidateEntity to the admin full-view DTO (without document).
func ToAdminCandidateResponseDTO(e entities.CandidateEntity) response.AdminCandidateResponseDTO {
	return response.AdminCandidateResponseDTO{
		ID:                e.CandidateID,
		AccountID:         e.CandidateAccountID,
		FullName:          e.CandidateFullName,
		FullNameCN:        e.CandidateFullNameCN,
		DOB:               e.CandidateDOB,
		Gender:            string(e.CandidateGender),
		Ethnicity:         e.CandidateEthnicity,
		Religion:          e.CandidateReligion,
		Nationality:       e.CandidateNationality,
		ChineseStudyYears: e.CandidateChineseStudyYears,
	}
}

// ToDocumentResponseDTO maps a CandidateDocumentEntity to its public DTO.
func ToDocumentResponseDTO(e entities.CandidateDocumentEntity) response.DocumentResponseDTO {
	return response.DocumentResponseDTO{
		ID:                 e.CandidateDocumentID,
		CandidateID:        e.CandidateDocumentCandidateID,
		DocType:            string(e.CandidateDocumentType),
		DocNumber:          e.CandidateDocumentNumber,
		IssueDate:          e.CandidateDocumentIssueDate,
		IssuePlace:         e.CandidateDocumentIssuePlace,
		FrontImageURL:      e.CandidateDocumentFrontImageURL,
		BackImageURL:       e.CandidateDocumentBackImageURL,
		PortraitImageURL:   e.CandidateDocumentPortraitImageURL,
		VerificationStatus: string(e.CandidateDocumentVerificationStatus),
		RejectionReason:    e.CandidateDocumentRejectionReason,
	}
}

// ToDocumentResponseDTOList maps a slice of CandidateDocumentEntity to a slice of DocumentResponseDTO.
func ToDocumentResponseDTOList(entities []entities.CandidateDocumentEntity) []response.DocumentResponseDTO {
	dtos := make([]response.DocumentResponseDTO, len(entities))
	for i, e := range entities {
		dtos[i] = ToDocumentResponseDTO(e)
	}
	return dtos
}
