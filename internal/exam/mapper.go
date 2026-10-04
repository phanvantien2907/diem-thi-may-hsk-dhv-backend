package exam

import "github.com/hskvinh/backend/internal/exam/dto/response"

// ToExamTypeResponseDTO maps an ExamTypeEntity to its public DTO.
func ToExamTypeResponseDTO(e ExamTypeEntity) response.ExamTypeResponseDTO {
	return response.ExamTypeResponseDTO{
		ID:          e.ExamTypeID,
		Code:        e.ExamTypeCode,
		Name:        e.ExamTypeName,
		Description: e.ExamTypeDescription,
	}
}

// ToExamSessionResponseDTO maps an ExamSessionEntity to its public DTO.
func ToExamSessionResponseDTO(e ExamSessionEntity) response.ExamSessionResponseDTO {
	return response.ExamSessionResponseDTO{
		ID:                   e.ExamSessionID,
		ExamTypeID:           e.ExamSessionExamTypeID,
		Date:                 e.ExamSessionDate,
		Shift:                string(e.ExamSessionShift),
		Capacity:             e.ExamSessionCapacity,
		Fee:                  e.ExamSessionFee,
		RegistrationDeadline: e.ExamSessionRegistrationDeadline,
		Status:               string(e.ExamSessionStatus),
		PublicationStatus:    string(e.ExamSessionPublicationStatus),
		PublishAt:            e.ExamSessionPublishAt,
		CheckInAt:            e.ExamSessionCheckInAt,
	}
}

// ToExamSeatResponseDTO maps an ExamSeatEntity to its public DTO.
func ToExamSeatResponseDTO(e ExamSeatEntity) response.ExamSeatResponseDTO {
	return response.ExamSeatResponseDTO{
		ID:        e.ExamSeatID,
		SessionID: e.ExamSeatExamSessionID,
		Number:    e.ExamSeatNumber,
		Status:    string(e.ExamSeatStatus),
		HeldUntil: e.ExamSeatHeldUntil,
	}
}
