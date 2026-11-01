package response

// AdmissionSlipResponseDTO carries all data needed to render an admission slip.
type AdmissionSlipResponseDTO struct {
	RegistrationID int64  `json:"registration_id"`
	CandidateName  string `json:"candidate_name"`
	IDNumber       string `json:"id_number"` // CCCD or passport
	ExamDate       string `json:"exam_date"` // YYYY-MM-DD
	Shift          string `json:"shift"`
	RoomName       string `json:"room_name"`
	RoomLocation   string `json:"room_location"`
	SeatNumber     int    `json:"seat_number"`
	ExamTypeName   string `json:"exam_type_name"`
}
