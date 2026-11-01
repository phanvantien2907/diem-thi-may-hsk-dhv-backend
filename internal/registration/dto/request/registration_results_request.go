package request

// AdminListPeriodResultsRequestDTO holds query filters for listing exam periods (batches) with slot summaries.
type AdminListPeriodResultsRequestDTO struct {
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
	Status string `form:"status"` // draft | published | registration_open | registration_closed | completed | cancelled
	Search string `form:"search"` // Tìm theo mã hoặc tên đợt thi
}

// AdminListSlotRegistrationsRequestDTO holds query filters for listing registered candidates inside an exam slot.
type AdminListSlotRegistrationsRequestDTO struct {
	Page               int    `form:"page"`
	Limit              int    `form:"limit"`
	Search             string `form:"search"`              // Tìm theo họ tên, email, SĐT, CCCD
	RegistrationStatus string `form:"registration_status"` // pending_payment | confirmed | cancelled | all
	VerificationStatus string `form:"verification_status"` // pending | verified | rejected | missing | all
	Eligibility        string `form:"eligibility"`         // eligible | ineligible | pending_review | all
	SlotScope          string `form:"slot_scope"`          // single | multiple | all
}

// AdminExportRegistrationsRequestDTO holds query filters for exporting registration lists to PDF or Excel (CSV).
type AdminExportRegistrationsRequestDTO struct {
	PeriodID           int64  `form:"period_id"`
	SessionID          int64  `form:"session_id"`
	Format             string `form:"format" validate:"required,oneof=pdf excel csv"` // pdf | excel | csv
	Eligibility        string `form:"eligibility" validate:"omitempty"`               // eligible | ineligible | pending_review | all
	RegistrationStatus string `form:"registration_status" validate:"omitempty"`       // confirmed | pending_payment | all
	SlotScope          string `form:"slot_scope" validate:"omitempty"`                // single | multiple | all
	Search             string `form:"search" validate:"omitempty"`
	SearchQuery        string `form:"search_query" validate:"omitempty"`
}

// AdminCancelRegistrationRequestDTO is the payload for an admin cancelling a candidate registration.
type AdminCancelRegistrationRequestDTO struct {
	Reason       string   `json:"reason" validate:"required,min=3"`
	EvidenceURLs []string `json:"evidence_urls"`
}

// AdminTransferRegistrationRequestDTO is the payload for an admin switching a candidate's exam slot.
type AdminTransferRegistrationRequestDTO struct {
	NewExamSessionID int64  `json:"new_exam_session_id" validate:"required,min=1"`
	Reason           string `json:"reason"`
}

// AdminListWaitlistRequestDTO holds pagination parameters for viewing a session's waitlist queue.
type AdminListWaitlistRequestDTO struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

