package response

import "time"

// PeriodRegistrationSummaryDTO summarizes an examination batch (exam_periods) and its individual exam slots.
type PeriodRegistrationSummaryDTO struct {
	PeriodID               int64                      `json:"period_id"`
	PeriodCode             string                     `json:"period_code"`
	PeriodName             string                     `json:"period_name"`
	Status                 string                     `json:"status"`
	EffectiveStatus        string                     `json:"effective_status"`
	StartDate              string                     `json:"start_date"`
	EndDate                string                     `json:"end_date"`
	RegistrationStart      *time.Time                 `json:"registration_start,omitempty"`
	RegistrationDeadline   *time.Time                 `json:"registration_deadline,omitempty"`
	TotalSlots             int                        `json:"total_slots"`
	TotalCapacity          int                        `json:"total_capacity"`
	TotalConfirmed         int                        `json:"total_confirmed"`
	TotalPendingPayment    int                        `json:"total_pending_payment"`
	TotalCancelled         int                        `json:"total_cancelled"`
	TotalBookedSeats       int                        `json:"total_booked_seats"`
	TotalHeldSeats         int                        `json:"total_held_seats"`
	TotalAvailableSeats    int                        `json:"total_available_seats"`
	TotalWaitlist          int                        `json:"total_waitlist"`
	MultiSlotCandidates    int                        `json:"multi_slot_candidates"`
	SingleSlotCandidates   int                        `json:"single_slot_candidates"`
	EligibleConfirmedCount int                        `json:"eligible_confirmed_count"`
	IneligibleActiveCount  int                        `json:"ineligible_active_count"`
	DiscrepancyCount       int                        `json:"discrepancy_count"`
	Alerts                 []SessionAlertDTO          `json:"alerts"`
	Slots                  []SlotRegistrationStatsDTO `json:"slots"`
}

// SlotRegistrationStatsDTO provides real-time enrollment statistics, eligibility breakdown, alerts, and discrepancies for a single exam slot.
type SlotRegistrationStatsDTO struct {
	SessionID           int64               `json:"session_id"`
	PeriodID            *int64              `json:"period_id,omitempty"`
	ExamTypeID          int64               `json:"exam_type_id"`
	ExamTypeCode        string              `json:"exam_type_code"`
	ExamTypeName        string              `json:"exam_type_name"`
	RoomID              *int64              `json:"room_id,omitempty"`
	RoomName            string              `json:"room_name"`
	ExamDate            string              `json:"exam_date"`
	Shift               string              `json:"shift"`
	ExamTimeStart       *string             `json:"exam_time_start,omitempty"`
	ExamTimeEnd         *string             `json:"exam_time_end,omitempty"`
	Fee                 int64               `json:"fee"`
	Status              string              `json:"status"`
	EffectiveStatus     string              `json:"effective_status"`
	Capacity            int                 `json:"capacity"`
	ConfirmedCount      int                 `json:"confirmed_count"`
	PendingPaymentCount int                 `json:"pending_payment_count"`
	CancelledCount      int                 `json:"cancelled_count"`
	AttendedCount       int                 `json:"attended_count"`
	BookedSeats         int                 `json:"booked_seats"`
	HeldSeats           int                 `json:"held_seats"`
	AvailableSeats      int                 `json:"available_seats"`
	WaitlistCount       int                 `json:"waitlist_count"`
	FillRatePercent     float64             `json:"fill_rate_percent"`
	SingleSlotCount     int                 `json:"single_slot_count"`
	MultiSlotCount      int                 `json:"multi_slot_count"`
	EligibleCount       int                 `json:"eligible_count"`
	IneligibleCount     int                 `json:"ineligible_count"`
	PendingReviewCount  int                 `json:"pending_review_count"`
	HasDiscrepancy      bool                `json:"has_discrepancy"`
	Discrepancy         *SlotDiscrepancyDTO `json:"discrepancy,omitempty"`
	Alerts              []SessionAlertDTO   `json:"alerts"`
}

// SlotDiscrepancyDTO describes seat/registration/payment mismatches detected in an exam slot.
type SlotDiscrepancyDTO struct {
	SessionID              int64  `json:"session_id"`
	PeriodID               *int64 `json:"period_id,omitempty"`
	ExamTypeName           string `json:"exam_type_name"`
	ExamDate               string `json:"exam_date"`
	Shift                  string `json:"shift"`
	ConfirmedRegistrations int    `json:"confirmed_registrations"`
	BookedSeats            int    `json:"booked_seats"`
	ConfirmedSeatDiff      int    `json:"confirmed_seat_diff"` // ConfirmedRegistrations - BookedSeats
	PendingPaymentRegs     int    `json:"pending_payment_registrations"`
	HeldSeats              int    `json:"held_seats"`
	PendingHoldDiff        int    `json:"pending_hold_diff"` // PendingPaymentRegs - HeldSeats
	UnpaidConfirmedRegs    int    `json:"unpaid_confirmed_registrations"`
	Message                string `json:"message"`
}

// SessionAlertDTO represents an operational warning or critical alert for an exam slot.
type SessionAlertDTO struct {
	Type      string `json:"type"`     // nearly_full | too_few_registrants | ineligible_docs_near_exam | seat_discrepancy
	Severity  string `json:"severity"` // info | warning | critical
	SessionID int64  `json:"session_id"`
	PeriodID  *int64 `json:"period_id,omitempty"`
	Message   string `json:"message"`
}

// SlotRegisteredCandidateItemDTO represents a candidate registered in an exam slot (with PII masked per Decree 13/2023).
type SlotRegisteredCandidateItemDTO struct {
	RegistrationID       int64                     `json:"registration_id"`
	SessionID            int64                     `json:"session_id"`
	PeriodID             *int64                    `json:"period_id,omitempty"`
	SeatID               int64                     `json:"seat_id"`
	SeatNumber           string                    `json:"seat_number"`
	SeatStatus           string                    `json:"seat_status"`
	RegistrationStatus   string                    `json:"registration_status"`
	RegisteredAt         time.Time                 `json:"registered_at"`
	IsAttended           bool                      `json:"is_attended"`
	CandidateID          int64                     `json:"candidate_id"`
	AccountID            int64                     `json:"account_id"`
	FullName             string                    `json:"full_name"`
	FullNameCN           *string                   `json:"full_name_cn,omitempty"`
	DOB                  time.Time                 `json:"dob"`
	Gender               string                    `json:"gender"`
	MaskedPhone          string                    `json:"masked_phone"`
	Email                string                    `json:"email"`
	DocID                *int64                    `json:"doc_id,omitempty"`
	DocType              string                    `json:"doc_type"`
	MaskedDocNumber      string                    `json:"masked_doc_number"`
	VerificationStatus   string                    `json:"verification_status"` // pending | verified | rejected | missing
	RejectionReason      *string                   `json:"rejection_reason,omitempty"`
	HasPortraitPhoto     bool                      `json:"has_portrait_photo"`
	HasIdentityPhotos    bool                      `json:"has_identity_photos"`
	PaymentID            *int64                    `json:"payment_id,omitempty"`
	PaymentStatus        string                    `json:"payment_status"` // success | pending | pending_refund | refunded | cancelled | none
	PaymentAmount        *int64                    `json:"payment_amount,omitempty"`
	EligibilityStatus    string                    `json:"eligibility_status"` // eligible | pending_review | ineligible
	EligibilityReasons   []string                  `json:"eligibility_reasons"`
	RegisteredSlotCount  int                       `json:"registered_slot_count"`
	IsMultiSlot          bool                      `json:"is_multi_slot"`
	RegisteredSessionIDs []int64                   `json:"registered_session_ids"`
	ProfileShortcuts     CandidateShortcutLinksDTO `json:"profile_shortcuts"`
}

// CandidateShortcutLinksDTO provides direct API endpoints to the Candidate Profile module so the UI can inspect and review documents without duplicating logic.
type CandidateShortcutLinksDTO struct {
	DetailAndAcquireLockURL string `json:"detail_and_acquire_lock_url"` // GET /api/v1/admin/candidates/:id
	ApproveDocumentURL      string `json:"approve_document_url"`        // POST /api/v1/admin/candidates/:id/approve
	RejectDocumentURL       string `json:"reject_document_url"`         // POST /api/v1/admin/candidates/:id/reject
	RevokeApprovalURL       string `json:"revoke_approval_url"`         // POST /api/v1/admin/candidates/:id/revoke-approval
	ReopenReviewURL         string `json:"reopen_review_url"`           // POST /api/v1/admin/candidates/:id/reopen-review
	ReleaseLockURL          string `json:"release_lock_url"`            // DELETE /api/v1/admin/candidates/:id/lock
}

// AdminWaitlistItemDTO represents an entry in an exam slot's waiting queue with position and masked PII.
type AdminWaitlistItemDTO struct {
	WaitlistID          int64                     `json:"waitlist_id"`
	SessionID           int64                     `json:"session_id"`
	QueuePosition       int                       `json:"queue_position"`
	JoinedAt            time.Time                 `json:"joined_at"`
	CandidateID         int64                     `json:"candidate_id"`
	AccountID           int64                     `json:"account_id"`
	FullName            string                    `json:"full_name"`
	DOB                 time.Time                 `json:"dob"`
	Gender              string                    `json:"gender"`
	MaskedPhone         string                    `json:"masked_phone"`
	Email               string                    `json:"email"`
	MaskedDocNumber     string                    `json:"masked_doc_number"`
	VerificationStatus  string                    `json:"verification_status"`
	RegisteredSlotCount int                       `json:"registered_slot_count"`
	IsMultiSlot         bool                      `json:"is_multi_slot"`
	ProfileShortcuts    CandidateShortcutLinksDTO `json:"profile_shortcuts"`
}

// AdminCancelRegistrationResponseDTO summarizes the result of cancelling a registration and any triggered refund workflow.
type AdminCancelRegistrationResponseDTO struct {
	RegistrationID int64  `json:"registration_id"`
	SessionID      int64  `json:"session_id"`
	SeatID         int64  `json:"seat_id"`
	Status         string `json:"status"`
	PaymentID      *int64 `json:"payment_id,omitempty"`
	PaymentStatus  string `json:"payment_status"`
	RefundAmount   *int64 `json:"refund_amount,omitempty"`
	Message        string `json:"message"`
}
