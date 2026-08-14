package result

import "github.com/hskvinh/backend/internal/result/dto/response"

// ToExamResultResponseDTO maps an ExamResultEntity to its public DTO.
func ToExamResultResponseDTO(e ExamResultEntity) response.ExamResultResponseDTO {
	var status *string
	if e.ExamResultStatus != nil {
		s := string(*e.ExamResultStatus)
		status = &s
	}
	return response.ExamResultResponseDTO{
		ID:             e.ExamResultID,
		RegistrationID: e.ExamResultExamRegistrationID,
		Listening:      e.ExamResultListeningScore,
		Reading:        e.ExamResultReadingScore,
		Writing:        e.ExamResultWritingScore,
		Total:          e.ExamResultTotalScore,
		Status:         status,
		PublishedAt:    e.ExamResultPublishedAt,
		CreatedAt:      e.ExamResultCreatedAt,
	}
}

// ToRecheckResponseDTO maps an ExamRecheckRequestEntity to its public DTO.
func ToRecheckResponseDTO(e ExamRecheckRequestEntity) response.RecheckResponseDTO {
	return response.RecheckResponseDTO{
		ID:            e.ExamRecheckID,
		ExamResultID:  e.ExamRecheckExamResultID,
		Reason:        e.ExamRecheckReason,
		Status:        string(e.ExamRecheckStatus),
		ReviewedAt:    e.ExamRecheckReviewedAt,
		NewTotalScore: e.ExamRecheckNewTotalScore,
		CreatedAt:     e.ExamRecheckCreatedAt,
	}
}
