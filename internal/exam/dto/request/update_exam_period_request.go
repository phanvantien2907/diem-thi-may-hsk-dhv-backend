package request

// UpdateExamPeriodRequestDTO carries fields for editing an exam period.
type UpdateExamPeriodRequestDTO struct {
	Name                 *string `json:"name"`
	Description          *string `json:"description"`
	Instructions         *string `json:"instructions"`
	StartDate            *string `json:"start_date"`
	EndDate              *string `json:"end_date"`
	RegistrationStart    *string `json:"registration_start"`
	RegistrationDeadline *string `json:"registration_deadline"`
}
