package request

// CreateStaffRequestDTO is the admin payload for POST /admin/staff-accounts.
type CreateStaffRequestDTO struct {
	Username string `json:"username"  validate:"required,min=3,max=50,alphanum"`
	Email    string `json:"email"     validate:"required,email,max=150"`
	Phone    string `json:"phone"     validate:"omitempty,max=20"`
	RoleCode string `json:"role_code" validate:"required"`
}
