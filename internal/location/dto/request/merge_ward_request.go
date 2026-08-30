package request

// MergeWardRequestDTO is the payload for POST /admin/locations/wards/:id/merge.
// TargetWardID is the ward that survives after the merge.
// All candidate records pointing to the source ward are re-pointed to the target.
type MergeWardRequestDTO struct {
	TargetWardID int64  `json:"target_ward_id" validate:"required,min=1"`
	Reason       string `json:"reason"         validate:"required,min=5,max=500"`
}
