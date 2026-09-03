package request

// CreateStaffRequestDTO is the admin payload for POST /admin/staff-accounts.
type CreateStaffRequestDTO struct {
	Cccd     string `json:"cccd"      validate:"required,len=12,numeric"`
	FullName string `json:"full_name" validate:"required,max=150"`
	Email    string `json:"email"     validate:"required,email,max=150"`
	Phone    string `json:"phone"     validate:"omitempty,max=20"`
	RoleCode string `json:"role_code" validate:"required"`
}
