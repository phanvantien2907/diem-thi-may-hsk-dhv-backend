package request

// DeactivateMyAccountRequestDTO requires explicit identity confirmation and password verification
// before a user can deactivate their own account.
type DeactivateMyAccountRequestDTO struct {
	ConfirmIdentity string `json:"confirm_identity" validate:"required,min=2,max=150"`
	Password        string `json:"password" validate:"required"`
	Reason          string `json:"reason" validate:"omitempty,max=500"`
}

// DeactivateAccountByAdminRequestDTO requires explicit confirmation (matching account_id and
// username/email/full_name) and a mandatory reason before an admin can deactivate an account.
type DeactivateAccountByAdminRequestDTO struct {
	ConfirmAccountID int64  `json:"confirm_account_id" validate:"required,gt=0"`
	ConfirmIdentity  string `json:"confirm_identity" validate:"required,min=2,max=150"`
	Reason           string `json:"reason" validate:"required,min=5,max=500"`
	PaidResolution   string `json:"paid_resolution" validate:"omitempty,oneof=block refund"`
}

// ReactivateAccountByAdminRequestDTO carries the mandatory reason for reactivating a disabled account.
type ReactivateAccountByAdminRequestDTO struct {
	Reason string `json:"reason" validate:"required,min=5,max=500"`
}

