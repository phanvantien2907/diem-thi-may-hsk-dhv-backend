package request

// AdminReopenReviewRequestDTO carries the mandatory reason when an admin re-opens review
// for a rejected candidate profile (transitioning rejected -> pending).
type AdminReopenReviewRequestDTO struct {
	Reason string `json:"reason" validate:"required,min=5,max=500"`
}

