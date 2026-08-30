package request

// ForceChangePasswordRequestDTO is the payload for POST /auth/force-password-change.
// Used by staff who received a temporary password from admin.
type ForceChangePasswordRequestDTO struct {
	TempToken   string `json:"temp_token"   validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8,max=72"`
}
