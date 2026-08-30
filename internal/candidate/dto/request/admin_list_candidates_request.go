package request

// AdminListCandidatesRequestDTO is the query for GET /admin/candidates.
type AdminListCandidatesRequestDTO struct {
	Status string `form:"status" validate:"omitempty,oneof=pending verified rejected"`
	Page   int    `form:"page"   validate:"omitempty,min=1"`
	Limit  int    `form:"limit"  validate:"omitempty,min=1,max=100"`
}
