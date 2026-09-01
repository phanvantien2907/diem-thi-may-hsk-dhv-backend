package response

// GenerateSeatsResponseDTO is returned after admin seat generation.
type GenerateSeatsResponseDTO struct {
	SessionID    int64 `json:"session_id"`
	CreatedSeats int   `json:"created_seats"`
}
