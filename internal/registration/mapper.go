package registration

// ToRegistrationResponseDTO maps an ExamRegistrationEntity to its public DTO.
func ToRegistrationResponseDTO(e ExamRegistrationEntity) ExamRegistrationResponseDTO {
	return ExamRegistrationResponseDTO{
		ID:           e.ExamRegistrationID,
		CandidateID:  e.ExamRegistrationCandidateID,
		SessionID:    e.ExamRegistrationExamSessionID,
		SeatID:       e.ExamRegistrationExamSeatID,
		Status:       string(e.ExamRegistrationStatus),
		RegisteredAt: e.ExamRegistrationRegisteredAt,
		CreatedAt:    e.ExamRegistrationCreatedAt,
	}
}
