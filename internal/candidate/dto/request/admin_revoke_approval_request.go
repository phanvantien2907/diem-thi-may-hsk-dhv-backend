package request

// AdminRevokeApprovalRequestDTO carries the mandatory reason and paid registration resolution strategy
// when an admin revokes a previously verified candidate profile.
type AdminRevokeApprovalRequestDTO struct {
	Reason         string `json:"reason" validate:"required,min=5,max=500"`
	PaidResolution string `json:"paid_resolution" validate:"omitempty,oneof=block refund"`
}

