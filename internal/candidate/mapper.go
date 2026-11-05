package candidate

import (
	"strings"

	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/utils"
)

// MaskPII masks a sensitive identifier (CCCD, Passport, or Phone number) in accordance
// with Decree 13/2023/ND-CP, preserving at most the first 3 and last 3 characters (e.g., "040***060").
func MaskPII(raw string) string {
	return utils.MaskPII(raw)
}

// MaskDocumentNumber masks a Citizen ID (CCCD) or Passport number for list endpoints (e.g., "040***060").
func MaskDocumentNumber(docNumber string) string {
	return utils.MaskDocumentNumber(docNumber)
}

// MaskPhoneNumber masks a phone number for list endpoints (e.g., "090***567").
func MaskPhoneNumber(phone string) string {
	return utils.MaskPhoneNumber(phone)
}

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
		Birthplace:        e.CandidateBirthplace,
		Nationality:       e.CandidateNationality,
		MotherTongue:      e.CandidateMotherTongue,
		ChineseStudyYears: e.CandidateChineseStudyYears,
	}
}

func ToAdminDocumentResponseDTO(e entities.CandidateDocumentEntity) response.AdminDocumentResponseDTO {
	hasFront := strings.TrimSpace(e.CandidateDocumentFrontImageURL) != ""
	hasBack := e.CandidateDocumentBackImageURL != nil && strings.TrimSpace(*e.CandidateDocumentBackImageURL) != ""
	hasPortrait := strings.TrimSpace(e.CandidateDocumentPortraitImageURL) != ""
	return response.AdminDocumentResponseDTO{
		ID: e.CandidateDocumentID, CandidateID: e.CandidateDocumentCandidateID,
		DocType: string(e.CandidateDocumentType), DocNumber: e.CandidateDocumentNumber,
		IssueDate: e.CandidateDocumentIssueDate, IssuePlace: e.CandidateDocumentIssuePlace,
		FrontImageURL: e.CandidateDocumentFrontImageURL, BackImageURL: e.CandidateDocumentBackImageURL,
		PortraitImageURL:   e.CandidateDocumentPortraitImageURL,
		HasFrontImage:      hasFront,
		HasBackImage:       hasBack,
		HasPortraitImage:   hasPortrait,
		PiiLocked:          false,
		VerificationStatus: string(e.CandidateDocumentVerificationStatus),
		RejectionReason:    e.CandidateDocumentRejectionReason,
		WardID:             e.CandidateDocumentWardID, ProvinceID: e.CandidateDocumentProvinceID,
		AddressDetail: e.CandidateDocumentAddressDetail,
	}
}

// ToAdminMaskedDocumentResponseDTO maps a CandidateDocumentEntity for list endpoints with DocNumber masked
// and raw PII image URLs withheld until the admin unlocks the Zero-Trust 6-Digit PIN Gateway (Decree 13/2023).
func ToAdminMaskedDocumentResponseDTO(e entities.CandidateDocumentEntity) response.AdminDocumentResponseDTO {
	dto := ToAdminDocumentResponseDTO(e)
	dto.DocNumber = MaskDocumentNumber(dto.DocNumber)
	dto.FrontImageURL = ""
	dto.BackImageURL = nil
	dto.PortraitImageURL = ""
	dto.PiiLocked = true
	return dto
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
		WardID:             e.CandidateDocumentWardID,
		ProvinceID:         e.CandidateDocumentProvinceID,
		AddressDetail:      e.CandidateDocumentAddressDetail,
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

// ToPortraitValidationResponseDTO maps PortraitValidationResult to its response DTO.
func ToPortraitValidationResponseDTO(res *PortraitValidationResult) response.PortraitValidationResponseDTO {
	if res == nil {
		return response.PortraitValidationResponseDTO{Valid: true, Reasons: []string{}}
	}
	reasons := res.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return response.PortraitValidationResponseDTO{
		Valid:             res.Valid,
		HasFace:           res.HasFace,
		HasGlasses:        res.HasGlasses,
		IsWhiteBackground: res.IsWhiteBackground,
		IsBlurred:         res.IsBlurred,
		IsTooDark:         res.IsTooDark,
		Brightness:        res.Brightness,
		Reasons:           reasons,
		RejectionReason:   res.RejectionReason,
	}
}

