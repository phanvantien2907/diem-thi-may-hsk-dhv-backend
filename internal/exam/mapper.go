package exam

// ToExamTypeResponseDTO maps an ExamTypeEntity to its public DTO.
func ToExamTypeResponseDTO(e ExamTypeEntity) ExamTypeResponseDTO {
	return ExamTypeResponseDTO{
		ID:          e.ExamTypeID,
		Code:        e.ExamTypeCode,
		Name:        e.ExamTypeName,
		Description: e.ExamTypeDescription,
	}
}

// ToExamRoomResponseDTO maps an ExamRoomEntity to its public DTO.
func ToExamRoomResponseDTO(e ExamRoomEntity) ExamRoomResponseDTO {
	return ExamRoomResponseDTO{
		ID:       e.ExamRoomID,
		Name:     e.ExamRoomName,
		Location: e.ExamRoomLocation,
		Capacity: e.ExamRoomCapacity,
	}
}

// ToExamSessionResponseDTO maps an ExamSessionEntity to its public DTO.
func ToExamSessionResponseDTO(e ExamSessionEntity) ExamSessionResponseDTO {
	return ExamSessionResponseDTO{
		ID:                   e.ExamSessionID,
		ExamTypeID:           e.ExamSessionExamTypeID,
		ExamRoomID:           e.ExamSessionExamRoomID,
		Date:                 e.ExamSessionDate,
		Shift:                string(e.ExamSessionShift),
		Capacity:             e.ExamSessionCapacity,
		Fee:                  e.ExamSessionFee,
		RegistrationDeadline: e.ExamSessionRegistrationDeadline,
		Status:               string(e.ExamSessionStatus),
	}
}

// ToExamSeatResponseDTO maps an ExamSeatEntity to its public DTO.
func ToExamSeatResponseDTO(e ExamSeatEntity) ExamSeatResponseDTO {
	return ExamSeatResponseDTO{
		ID:        e.ExamSeatID,
		SessionID: e.ExamSeatExamSessionID,
		Number:    e.ExamSeatNumber,
		Status:    string(e.ExamSeatStatus),
		HeldUntil: e.ExamSeatHeldUntil,
	}
}
