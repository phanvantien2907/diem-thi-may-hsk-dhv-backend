package request

// AdminListCandidatesRequestDTO is the query for GET /admin/candidates.
type AdminListCandidatesRequestDTO struct {
	Status   string `form:"status" validate:"omitempty,oneof=all pending verified rejected"`
	Level    string `form:"level" validate:"omitempty,max=100"`
	Search   string `form:"search" validate:"omitempty,max=100"`
	ExamDate string `form:"exam_date" validate:"omitempty,len=10"`
	Page     int    `form:"page"   validate:"omitempty,min=1"`
	Limit    int    `form:"limit"  validate:"omitempty,min=1,max=100"`
}

// AdminCandidateAuditLogsRequestDTO is the query for GET /admin/candidates/:id/audit-logs.
type AdminCandidateAuditLogsRequestDTO struct {
	Scope string `form:"scope" validate:"omitempty,oneof=all candidate_documents candidates"`
	Page  int    `form:"page"  validate:"omitempty,min=1"`
	Limit int    `form:"limit" validate:"omitempty,min=1,max=100"`
}
