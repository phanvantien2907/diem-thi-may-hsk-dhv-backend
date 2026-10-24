package request

// RescheduleExamPeriodRequestDTO carries parameters for postponing/rescheduling an exam period.
type RescheduleExamPeriodRequestDTO struct {
	NewStartDate            string `json:"new_start_date" validate:"required"`
	NewEndDate              string `json:"new_end_date"`
	NewRegistrationDeadline string `json:"new_registration_deadline"`
	DaysOffset              int    `json:"days_offset"`
	Reason                  string `json:"reason" validate:"required,min=5,max=500"`
}
