package response

// AdminCandidateMetricsResponseDTO contains summary counts for the admin candidate list.
type AdminCandidateMetricsResponseDTO struct {
	Total    int64 `json:"total"`
	Pending  int64 `json:"pending"`
	Verified int64 `json:"verified"`
	Rejected int64 `json:"rejected"`
}
