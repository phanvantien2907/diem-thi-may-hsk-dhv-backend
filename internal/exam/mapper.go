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
		PeriodID:             e.ExamSessionExamPeriodID,
		ExamTypeID:           e.ExamSessionExamTypeID,
		Name:                 e.ExamSessionName,
		Description:          e.ExamSessionDescription,
		Date:                 e.ExamSessionDate,
		Shift:                string(e.ExamSessionShift),
		Capacity:             e.ExamSessionCapacity,
		Fee:                  e.ExamSessionFee,
		RegistrationDeadline: e.ExamSessionRegistrationDeadline,
		Status:               string(e.ExamSessionStatus),
		PublicationStatus:    string(e.ExamSessionPublicationStatus),
		PublishAt:            e.ExamSessionPublishAt,
		CheckInAt:            e.ExamSessionCheckInAt,
		DuplicatedFromID:     e.ExamSessionDuplicatedFromID,
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

// ToExamPeriodResponseDTO maps an ExamPeriodEntity to its public DTO.
func ToExamPeriodResponseDTO(e ExamPeriodEntity, sessionCount int, sessions []response.ExamSessionResponseDTO) response.ExamPeriodResponseDTO {
	var startDate, endDate *string
	if e.ExamPeriodStartDate != nil {
		s := e.ExamPeriodStartDate.Format("2006-01-02")
		startDate = &s
	}
	if e.ExamPeriodEndDate != nil {
		s := e.ExamPeriodEndDate.Format("2006-01-02")
		endDate = &s
	}
	return response.ExamPeriodResponseDTO{
		ID:                   e.ExamPeriodID,
		Name:                 e.ExamPeriodName,
		Code:                 e.ExamPeriodCode,
		Description:          e.ExamPeriodDescription,
		Instructions:         e.ExamPeriodInstructions,
		StartDate:            startDate,
		EndDate:              endDate,
		RegistrationStart:    e.ExamPeriodRegistrationStart,
		RegistrationDeadline: e.ExamPeriodRegistrationDeadline,
		Status:               string(e.ExamPeriodStatus),
		SessionCount:         sessionCount,
		Sessions:             sessions,
		CreatedAt:            e.ExamPeriodCreatedAt,
		UpdatedAt:            e.ExamPeriodUpdatedAt,
	}
}
