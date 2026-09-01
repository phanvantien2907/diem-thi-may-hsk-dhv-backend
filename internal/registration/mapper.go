package registration

import (
	"github.com/hskvinh/backend/internal/registration/dto/response"
	"github.com/hskvinh/backend/internal/registration/entities"
)

// ToRegistrationResponseDTO maps an ExamRegistrationEntity to its public DTO.
func ToRegistrationResponseDTO(e entities.ExamRegistrationEntity) response.ExamRegistrationResponseDTO {
	return response.ExamRegistrationResponseDTO{
		ID:           e.ExamRegistrationID,
		CandidateID:  e.ExamRegistrationCandidateID,
		SessionID:    e.ExamRegistrationExamSessionID,
		SeatID:       e.ExamRegistrationExamSeatID,
		Status:       string(e.ExamRegistrationStatus),
		RegisteredAt: e.ExamRegistrationRegisteredAt,
	}
}

// ToWaitlistResponseDTO maps an ExamWaitlistEntity to its public DTO.
func ToWaitlistResponseDTO(e entities.ExamWaitlistEntity) response.WaitlistResponseDTO {
	return response.WaitlistResponseDTO{
		ID:        e.ExamWaitlistID,
		SessionID: e.ExamWaitlistExamSessionID,
		JoinedAt:  e.ExamWaitlistJoinedAt,
	}
}
