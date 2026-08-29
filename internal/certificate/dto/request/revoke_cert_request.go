package request

// RevokeCertificateRequestDTO is the admin payload for POST /admin/certificates/:id/revoke.
type RevokeCertificateRequestDTO struct {
	Reason string `json:"reason" validate:"required,max=500"`
}
