package request

// UpdateExamPeriodRequestDTO carries fields for editing an exam period.
//
// Draft and published periods accept every field. Once registration is open or
// closed only Description and Instructions remain editable; dates must go through
// the reschedule workflow so candidates get notified.
type UpdateExamPeriodRequestDTO struct {
	Name                 *string `json:"name"                  validate:"omitempty,max=255"`
	Description          *string `json:"description"           validate:"omitempty"`
	Instructions         *string `json:"instructions"          validate:"omitempty"`
	StartDate            *string `json:"start_date"            validate:"omitempty"` // YYYY-MM-DD
	EndDate              *string `json:"end_date"              validate:"omitempty"` // YYYY-MM-DD
	RegistrationStart    *string `json:"registration_start"    validate:"omitempty"` // RFC3339
	RegistrationDeadline *string `json:"registration_deadline" validate:"omitempty"` // RFC3339
}

// HasScheduleChange reports whether the request touches any date or the registration window.
func (r UpdateExamPeriodRequestDTO) HasScheduleChange() bool {
	return r.StartDate != nil || r.EndDate != nil ||
		r.RegistrationStart != nil || r.RegistrationDeadline != nil
}

// HasAnyChange reports whether at least one field was supplied.
func (r UpdateExamPeriodRequestDTO) HasAnyChange() bool {
	return r.Name != nil || r.Description != nil || r.Instructions != nil || r.HasScheduleChange()
}
