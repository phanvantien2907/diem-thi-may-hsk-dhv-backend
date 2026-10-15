package request

// ValidatePortraitRequestDTO is the payload for POST /me/candidate-profile/validate-portrait.
type ValidatePortraitRequestDTO struct {
	PortraitImageURL string `json:"portrait_image_url" validate:"required,url"`
}
