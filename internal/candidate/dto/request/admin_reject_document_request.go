package request

// AdminRejectDocumentRequestDTO is the payload for POST /admin/candidates/:id/reject.
type AdminRejectDocumentRequestDTO struct {
	Reason string `json:"reason" validate:"required,max=500"`
}
