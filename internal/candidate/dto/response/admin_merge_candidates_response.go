package response

// AdminMergeCandidatesResponseDTO returns the summary produced by fn_merge_candidate_profiles.
type AdminMergeCandidatesResponseDTO struct {
	PrimaryCandidateID          int64  `json:"primary_candidate_id"`
	DuplicateCandidateID        int64  `json:"duplicate_candidate_id"`
	DuplicateAccountID          int64  `json:"duplicate_account_id"`
	MovedRegistrationsCount     int    `json:"moved_registrations_count"`
	CancelledRegistrationsCount int    `json:"cancelled_registrations_count"`
	Reason                      string `json:"reason"`
}

