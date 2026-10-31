package request

// AdminMergeCandidatesRequestDTO specifies the primary and duplicate candidate IDs to merge.
type AdminMergeCandidatesRequestDTO struct {
	PrimaryCandidateID   int64  `json:"primary_candidate_id" validate:"required,gt=0"`
	DuplicateCandidateID int64  `json:"duplicate_candidate_id" validate:"required,gt=0"`
	Reason               string `json:"reason" validate:"omitempty,max=500"`
}

