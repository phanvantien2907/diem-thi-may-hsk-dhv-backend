package response

import "time"

// ExamPeriodResponseDTO is the public exam period representation.
type ExamPeriodResponseDTO struct {
	ID                   int64                    `json:"id"`
	Name                 string                   `json:"name"`
	Code                 string                   `json:"code"`
	Description          *string                  `json:"description,omitempty"`
	Instructions         *string                  `json:"instructions,omitempty"`
	StartDate            *string                  `json:"start_date,omitempty"`
	EndDate              *string                  `json:"end_date,omitempty"`
	RegistrationStart    *time.Time               `json:"registration_start,omitempty"`
	RegistrationDeadline *time.Time               `json:"registration_deadline,omitempty"`
	Status               string                   `json:"status"`
	SessionCount         int                      `json:"session_count"`
	Sessions             []ExamSessionResponseDTO `json:"sessions,omitempty"`
	CreatedAt            time.Time                `json:"created_at"`
	UpdatedAt            time.Time                `json:"updated_at"`
}

// DuplicateExamPeriodResponseDTO contains the result of duplicating an exam period.
type DuplicateExamPeriodResponseDTO struct {
	NewPeriodID        int64                 `json:"new_period_id"`
	DuplicatedSessions int                   `json:"duplicated_sessions"`
	Period             ExamPeriodResponseDTO `json:"period"`
}

// RescheduleExamPeriodResponseDTO contains the result of rescheduling an exam period.
type RescheduleExamPeriodResponseDTO struct {
	PeriodID          int64                 `json:"period_id"`
	UpdatedSessions   int                   `json:"updated_sessions"`
	CandidatesAlerted int                   `json:"candidates_alerted"`
	Reason            string                `json:"reason"`
	Period            ExamPeriodResponseDTO `json:"period"`
}
