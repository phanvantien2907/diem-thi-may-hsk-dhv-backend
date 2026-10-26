package request

// UpdateExamSessionRequestDTO is a partial update of a single exam session.
// Only non-nil fields are applied. Date/shift/room/exam type are locked once the
// session has registrations; only Name and Description stay editable afterwards.
type UpdateExamSessionRequestDTO struct {
	Name                 *string `json:"name"                     validate:"omitempty,max=255"`
	Description          *string `json:"description"              validate:"omitempty"`
	Date                 *string `json:"date"                     validate:"omitempty"` // YYYY-MM-DD
	Shift                *string `json:"shift"                    validate:"omitempty,oneof=morning afternoon evening"`
	Capacity             *int    `json:"capacity"                 validate:"omitempty,min=1"`
	Fee                  *int64  `json:"fee"                      validate:"omitempty,min=0"`
	RegistrationDeadline *string `json:"registration_deadline"    validate:"omitempty"` // RFC3339
	CheckInAt            *string `json:"check_in_at"              validate:"omitempty"` // RFC3339
}

// HasScheduleChange reports whether the request touches fields that are locked once
// the session has registrations.
func (r UpdateExamSessionRequestDTO) HasScheduleChange() bool {
	return r.Date != nil || r.Shift != nil || r.Fee != nil || r.RegistrationDeadline != nil
}

// HasScheduleOrFeeChange keeps the historical name used by existing callers.
func (r UpdateExamSessionRequestDTO) HasScheduleOrFeeChange() bool { return r.HasScheduleChange() }

// HasAnyChange reports whether at least one field was supplied.
func (r UpdateExamSessionRequestDTO) HasAnyChange() bool {
	return r.Name != nil || r.Description != nil || r.HasScheduleChange() || r.Capacity != nil || r.CheckInAt != nil
}
