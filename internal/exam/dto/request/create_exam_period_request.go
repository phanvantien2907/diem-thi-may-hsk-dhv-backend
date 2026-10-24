package request

// CreateExamPeriodRequestDTO carries parameters to create a new exam period.
type CreateExamPeriodRequestDTO struct {
	Name                 string `json:"name" validate:"required,min=3,max=150"`
	Code                 string `json:"code" validate:"required,min=3,max=50"`
	Description          string `json:"description"`
	Instructions         string `json:"instructions"`
	StartDate            string `json:"start_date" validate:"omitempty"`
	EndDate              string `json:"end_date" validate:"omitempty"`
	RegistrationStart    string `json:"registration_start" validate:"omitempty"`
	RegistrationDeadline string `json:"registration_deadline" validate:"omitempty"`
}
