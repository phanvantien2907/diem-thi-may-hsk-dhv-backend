package response

// PortraitValidationResponseDTO is returned by POST /me/candidate-profile/validate-portrait.
type PortraitValidationResponseDTO struct {
	Valid             bool     `json:"valid"`
	HasFace           bool     `json:"has_face"`
	HasGlasses        bool     `json:"has_glasses"`
	IsWhiteBackground bool     `json:"is_white_background"`
	IsBlurred         bool     `json:"is_blurred"`
	IsTooDark         bool     `json:"is_too_dark"`
	Brightness        float64  `json:"brightness"`
	Reasons           []string `json:"reasons"`
	RejectionReason   string   `json:"rejection_reason"`
}
