package exam

import (
	"time"

	"github.com/hskvinh/backend/internal/exam/dto/response"
)

// ToExamTypeResponseDTO maps an ExamTypeEntity to its public DTO.
func ToExamTypeResponseDTO(e ExamTypeEntity) response.ExamTypeResponseDTO {
	return response.ExamTypeResponseDTO{
		ID:          e.ExamTypeID,
		Code:        e.ExamTypeCode,
		Name:        e.ExamTypeName,
		Description: e.ExamTypeDescription,
	}
}

// ToExamSessionResponseDTO maps a session plus its evaluated state to the public DTO.
// The exposed `status`/`closed_reason` are the effective values derived from now, never
// the persisted column, so a lagging background sync can never mislead the client.
func ToExamSessionResponseDTO(e ExamSessionEntity, st SessionState, now time.Time) response.ExamSessionResponseDTO {
	effective, reason := st.Effective(now)
	var closedReason *string
	if reason != "" {
		r := reason.String()
		closedReason = &r
	}
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
		Status:               effective.String(),
		StoredStatus:         e.ExamSessionStatus.String(),
		ClosedReason:         closedReason,
		PublishAt:            e.ExamSessionPublishAt,
		CheckInAt:            e.ExamSessionCheckInAt,
		DuplicatedFromID:     e.ExamSessionDuplicatedFromID,
		AvailableActions:     actionStrings(AvailableSessionActions(st, now)),
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

// ToExamPeriodResponseDTO maps a period plus its evaluated state to the public DTO.
func ToExamPeriodResponseDTO(e ExamPeriodEntity, st PeriodState, sessionCount int, sessions []response.ExamSessionResponseDTO, now time.Time) response.ExamPeriodResponseDTO {
	var startDate, endDate, originalDate *string
	if e.ExamPeriodStartDate != nil {
		d := e.ExamPeriodStartDate.Format("2006-01-02")
		startDate = &d
	}
	if e.ExamPeriodEndDate != nil {
		d := e.ExamPeriodEndDate.Format("2006-01-02")
		endDate = &d
	}
	if e.ExamPeriodOriginalDate != nil {
		d := e.ExamPeriodOriginalDate.Format("2006-01-02")
		originalDate = &d
	}
	effective := st.Effective(now)
	return response.ExamPeriodResponseDTO{
		ID:                   e.ExamPeriodID,
		Name:                 e.ExamPeriodName,
		Code:                 e.ExamPeriodCode,
		Description:          e.ExamPeriodDescription,
		Instructions:         e.ExamPeriodInstructions,
		StartDate:            startDate,
		EndDate:              endDate,
		OriginalDate:         originalDate,
		RescheduleCount:      e.ExamPeriodRescheduleCount,
		RegistrationStart:    e.ExamPeriodRegistrationStart,
		RegistrationDeadline: e.ExamPeriodRegistrationDeadline,
		Status:               effective.String(),
		StoredStatus:         e.ExamPeriodStatus.String(),
		SessionCount:         sessionCount,
		Sessions:             sessions,
		AvailableActions:     actionStrings(AvailablePeriodActions(st, now)),
		CreatedAt:            e.ExamPeriodCreatedAt,
		UpdatedAt:            e.ExamPeriodUpdatedAt,
	}
}

// newPeriodState builds the guard state from an entity.
func newPeriodState(e ExamPeriodEntity) PeriodState {
	return PeriodState{
		StoredStatus:         e.ExamPeriodStatus,
		RegistrationStart:    e.ExamPeriodRegistrationStart,
		RegistrationDeadline: e.ExamPeriodRegistrationDeadline,
		StartDate:            e.ExamPeriodStartDate,
		EndDate:              e.ExamPeriodEndDate,
	}
}
