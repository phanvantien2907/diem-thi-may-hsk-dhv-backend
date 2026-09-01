package response

// ExamRoomResponseDTO is the public exam room representation.
type ExamRoomResponseDTO struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Location *string `json:"location,omitempty"`
	Capacity int     `json:"capacity"`
}
