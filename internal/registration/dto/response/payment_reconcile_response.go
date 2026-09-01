package response

// PaymentReconcileItemDTO is one row in the reconcile report.
type PaymentReconcileItemDTO struct {
	PaymentID      int64   `json:"payment_id"`
	RegistrationID int64   `json:"registration_id"`
	CandidateName  string  `json:"candidate_name"`
	Amount         float64 `json:"amount"`
	Method         string  `json:"method"`
	TransactionRef string  `json:"transaction_ref"`
	Status         string  `json:"status"`
	PaidAt         *string `json:"paid_at"`
}
