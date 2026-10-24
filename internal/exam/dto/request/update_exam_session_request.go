package request

// UpdateExamSessionRequestDTO is a partial update of a single exam session.
// Only non-nil fields are applied. Intended for the "edit draft period" screen that the
// admin lands on after duplicating a period (dates are NULL and must be re-entered).
type UpdateExamSessionRequestDTO struct {
	Date                 *string `json:"date"                     validate:"omitempty"` // YYYY-MM-DD
	Shift                *string `json:"shift"                    validate:"omitempty,oneof=morning afternoon evening"`
	Capacity             *int    `json:"capacity"                 validate:"omitempty,min=1"`
	Fee                  *int64  `json:"fee"                      validate:"omitempty,min=0"`
	RegistrationDeadline *string `json:"registration_deadline"    validate:"omitempty"` // RFC3339
	CheckInAt            *string `json:"exam_session_check_in_at" validate:"omitempty"` // RFC3339
}

// HasScheduleOrFeeChange reports whether the request touches fields that, outside of draft,
// must go through the reschedule workflow instead of a direct edit.
func (r UpdateExamSessionRequestDTO) HasScheduleOrFeeChange() bool {
	return r.Date != nil || r.RegistrationDeadline != nil || r.Fee != nil ||
		r.Shift != nil || r.Capacity != nil || r.CheckInAt != nil
}
