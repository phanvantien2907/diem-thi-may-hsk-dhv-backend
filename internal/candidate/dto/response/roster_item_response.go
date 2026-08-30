package response

import "time"

// RosterItemResponseDTO is one row in the exam attendance roster for proctors.
type RosterItemResponseDTO struct {
	SeatNumber       int16     `json:"seat_number"`
	CandidateID      int64     `json:"candidate_id"`
	FullName         string    `json:"full_name"`
	DOB              time.Time `json:"dob"`
	Gender           string    `json:"gender"`
	Nationality      string    `json:"nationality"`
	DocNumber        string    `json:"doc_number"`
	DocType          string    `json:"doc_type"`
	PortraitImageURL string    `json:"portrait_image_url"`
}
