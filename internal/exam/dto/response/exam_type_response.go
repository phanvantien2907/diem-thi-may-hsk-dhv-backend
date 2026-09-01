package response

// ExamTypeResponseDTO is the public exam type representation.
type ExamTypeResponseDTO struct {
	ID          int64   `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}
