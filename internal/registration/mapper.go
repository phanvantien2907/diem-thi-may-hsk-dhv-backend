package registration

import "github.com/hskvinh/backend/internal/registration/dto/response"

// ToRegistrationResponseDTO maps an ExamRegistrationEntity to its public DTO.
func ToRegistrationResponseDTO(e ExamRegistrationEntity) response.ExamRegistrationResponseDTO {
	return response.ExamRegistrationResponseDTO{
		ID:           e.ExamRegistrationID,
		CandidateID:  e.ExamRegistrationCandidateID,
		SessionID:    e.ExamRegistrationExamSessionID,
		SeatID:       e.ExamRegistrationExamSeatID,
		Status:       string(e.ExamRegistrationStatus),
		RegisteredAt: e.ExamRegistrationRegisteredAt,
		CreatedAt:    e.ExamRegistrationCreatedAt,
	}
}
