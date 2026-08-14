package candidate

// ToCandidateResponseDTO maps a CandidateEntity to its public DTO.
func ToCandidateResponseDTO(e CandidateEntity) CandidateResponseDTO {
	return CandidateResponseDTO{
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
		CreatedAt:         e.CandidateCreatedAt,
		UpdatedAt:         e.CandidateUpdatedAt,
	}
}

// ToDocumentResponseDTO maps a CandidateDocumentEntity to its public DTO.
func ToDocumentResponseDTO(e CandidateDocumentEntity) DocumentResponseDTO {
	return DocumentResponseDTO{
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
		CreatedAt:          e.CandidateDocumentCreatedAt,
		UpdatedAt:          e.CandidateDocumentUpdatedAt,
	}
}
