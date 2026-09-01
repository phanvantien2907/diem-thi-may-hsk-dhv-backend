package response

// SessionDashboardResponseDTO is the admin dashboard for a session.
type SessionDashboardResponseDTO struct {
	SessionID      int64  `json:"session_id"`
	Capacity       int    `json:"capacity"`
	Booked         int    `json:"booked"`
	Held           int    `json:"held"`
	Available      int    `json:"available"`
	WaitlistCount  int    `json:"waitlist_count"`
	AttendedCount  int    `json:"attended_count"`
	Status         string `json:"status"`
}
