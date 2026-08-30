package request

// ChangePasswordRequestDTO is the payload for PATCH /me/password.
type ChangePasswordRequestDTO struct {
	OldPassword string `json:"old_password" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8,max=72,matches=^[a-zA-Z0-9!@#$%^&*]*$"`
}
