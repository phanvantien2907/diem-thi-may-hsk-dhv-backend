package request

// CloseRegistrationRequestDTO closes a period's or a session's registration early.
// Reason is optional free text kept in the audit trail; the machine-readable
// ClosedReason drives the display of "Đã đóng" vs "Đã hết hạn".
type CloseRegistrationRequestDTO struct {
	Reason       string `json:"reason" validate:"omitempty,max=500"`
	ClosedReason string `json:"closed_reason" validate:"omitempty,oneof=manual full deadline period_closed"`
}
