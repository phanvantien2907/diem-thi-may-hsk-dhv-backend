package request

// RevenueChartRequestDTO holds query params for the revenue chart endpoint.
type RevenueChartRequestDTO struct {
	Range string `form:"range" validate:"required,oneof=7days 30days"`
}

// ExceptionListRequestDTO holds query params for the exception list endpoint.
type ExceptionListRequestDTO struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

// ManualApproveRequestDTO holds the body for manual-approve.
type ManualApproveRequestDTO struct {
	Reason       string   `json:"reason" validate:"required,min=5,max=500"`
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
}

// RefundMarkRequestDTO holds the body for refund-mark.
type RefundMarkRequestDTO struct {
	Reason       string   `json:"reason" validate:"required,min=5,max=500"`
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
}

// AuditLogMeRequestDTO holds query params for the admin's own audit log.
type AuditLogMeRequestDTO struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

// RecentLogsRequestDTO holds query params for recent system logs.
type RecentLogsRequestDTO struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}
