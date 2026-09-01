package request

// TransferRegistrationRequestDTO is the payload for POST /me/registrations/:id/transfer.
type TransferRegistrationRequestDTO struct {
	NewExamSessionID int64 `json:"new_exam_session_id" validate:"required,min=1"`
}
