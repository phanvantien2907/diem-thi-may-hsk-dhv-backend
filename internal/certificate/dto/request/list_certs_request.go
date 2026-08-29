package request

// ListCertificatesRequestDTO is the admin query for GET /admin/certificates.
type ListCertificatesRequestDTO struct {
	ExamSessionID *int64  `form:"exam_session_id" validate:"omitempty,min=1"`
	Status        string  `form:"status"          validate:"omitempty,oneof=issued revoked"`
	From          string  `form:"from"            validate:"omitempty"` // YYYY-MM-DD
	To            string  `form:"to"              validate:"omitempty"` // YYYY-MM-DD
	Page          int     `form:"page,default=1"  validate:"omitempty,min=1"`
	Limit         int     `form:"limit,default=20" validate:"omitempty,min=1,max=100"`
}
