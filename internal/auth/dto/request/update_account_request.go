package request

// UpdateAccountRequestDTO is the payload for PUT /me.
type UpdateAccountRequestDTO struct {
	Email string `json:"email" validate:"omitempty,email,max=150"`
	Phone string `json:"phone" validate:"omitempty,max=20"`
}
