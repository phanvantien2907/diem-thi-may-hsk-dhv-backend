package response

import "encoding/json"

// ─── 1. Metrics Overview ────────────────────────────────────────────────────

// MetricsOverviewResponseDTO returns the top 4 dashboard KPIs.
type MetricsOverviewResponseDTO struct {
	TotalRevenue       int64   `json:"total_revenue"`
	TotalRegistrations int64   `json:"total_registrations"`
	FillRate           float64 `json:"fill_rate"`
	PaymentSuccessRate float64 `json:"payment_success_rate"`
}

// ─── 2. Revenue Chart ───────────────────────────────────────────────────────

// RevenueChartPointDTO is a single data point on the revenue/registration chart.
type RevenueChartPointDTO struct {
	Date          string `json:"date"`
	Revenue       int64  `json:"revenue"`
	Registrations int64  `json:"registrations"`
}

// ─── 3. HSK Distribution ────────────────────────────────────────────────────

// HSKDistributionItemDTO is one slice of the pie chart.
type HSKDistributionItemDTO struct {
	Level string `json:"level"`
	Count int64  `json:"count"`
}

// ─── 4. Payment Funnel ──────────────────────────────────────────────────────

// PaymentFunnelItemDTO shows transaction volume per time slot.
type PaymentFunnelItemDTO struct {
	Hour      int   `json:"hour"`
	Success   int64 `json:"success"`
	Holding   int64 `json:"holding"`
	Cancelled int64 `json:"cancelled"`
}

// ─── 5. Live Session Status ─────────────────────────────────────────────────

// LiveSessionItemDTO shows real-time seat counts for an exam session.
type LiveSessionItemDTO struct {
	SessionID      int64  `json:"session_id"`
	ExamTypeName   string `json:"exam_type_name"`
	ExamTypeCode   string `json:"exam_type_code"`
	RoomName       string `json:"room_name"`
	SessionDate    string `json:"session_date"`
	Shift          string `json:"shift"`
	TotalSeats     int64  `json:"total_seats"`
	LockedSeats    int64  `json:"locked_seats"`
	HoldingSeats   int64  `json:"holding_seats"`
	AvailableSeats int64  `json:"available_seats"`
}

// ─── 6. Transaction Exceptions ──────────────────────────────────────────────

// TransactionExceptionItemDTO describes a problematic transaction.
type TransactionExceptionItemDTO struct {
	PaymentID      int64   `json:"payment_id"`
	RegistrationID int64   `json:"registration_id"`
	CandidateName  string  `json:"candidate_name"`
	ExpectedAmount int64   `json:"expected_amount"`
	ActualAmount   *int64  `json:"actual_amount"`
	Status         string  `json:"status"`
	ExceptionType  string  `json:"exception_type"`
	TransactionRef *string `json:"transaction_ref"`
	CreatedAt      string  `json:"created_at"`
}

// ─── 7. Manual Approve / Refund Mark ────────────────────────────────────────

// ManualApproveResponseDTO returned after a successful manual-approve.
type ManualApproveResponseDTO struct {
	PaymentID      int64  `json:"payment_id"`
	RegistrationID int64  `json:"registration_id"`
	NewStatus      string `json:"new_status"`
	Message        string `json:"message"`
}

// RefundMarkResponseDTO returned after marking a transaction for refund.
type RefundMarkResponseDTO struct {
	PaymentID int64  `json:"payment_id"`
	NewStatus string `json:"new_status"`
	Message   string `json:"message"`
}

// ─── 8. Recent Logs ─────────────────────────────────────────────────────────

// RecentLogItemDTO represents a recent system event.
type RecentLogItemDTO struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	EntityTable string `json:"entity_table"`
	EntityID    int64  `json:"entity_id"`
	ActorID     *int64 `json:"actor_id"`
	CreatedAt   string `json:"created_at"`
}

// ─── 9. Admin's own audit log ───────────────────────────────────────────────

// AdminAuditLogItemDTO represents one audit entry for the logged-in admin.
type AdminAuditLogItemDTO struct {
	ID          int64            `json:"id"`
	Action      string           `json:"action"`
	EntityTable string           `json:"entity_table"`
	EntityID    int64            `json:"entity_id"`
	OldValue    *json.RawMessage `json:"old_value"`
	NewValue    *json.RawMessage `json:"new_value"`
	CreatedAt   string           `json:"created_at"`
}

// ─── 10. Clear Cache ────────────────────────────────────────────────────────

// ClearCacheResponseDTO returned after clearing cache.
type ClearCacheResponseDTO struct {
	Message string `json:"message"`
}
