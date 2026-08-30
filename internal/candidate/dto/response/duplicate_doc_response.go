package response

// DuplicateDocResponseDTO represents candidates sharing the same document number.
type DuplicateDocResponseDTO struct {
	DocNumber    string  `json:"doc_number"`
	DocType      string  `json:"doc_type"`
	CandidateIDs []int64 `json:"candidate_ids"`
	Count        int     `json:"count"`
}
