package response

import "time"

// PaymentResponseDTO is the public payment representation.
type PaymentResponseDTO struct {
	ID             int64      `json:"id"`
	RegistrationID int64      `json:"exam_registration_id"`
	Amount         int64      `json:"amount"`
	Method         string     `json:"method"`
	TransactionRef *string    `json:"transaction_ref,omitempty"`
	Status         string     `json:"status"`
	CheckoutURL    *string    `json:"checkout_url,omitempty"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// AdminPaymentResponseDTO adds candidate/registration info for admin views.
type AdminPaymentResponseDTO struct {
	ID             int64      `json:"id"`
	RegistrationID int64      `json:"exam_registration_id"`
	Amount         int64      `json:"amount"`
	AmountReceived *int64     `json:"amount_received,omitempty"`
	Method         string     `json:"method"`
	Gateway        *string    `json:"gateway,omitempty"`
	TransactionRef *string    `json:"transaction_ref,omitempty"`
	GatewayTransID *string    `json:"gateway_trans_id,omitempty"`
	Content        *string    `json:"content,omitempty"`
	Status         string     `json:"status"`
	EvidenceURLs   []string   `json:"evidence_urls,omitempty"`
	AdminNote      *string    `json:"admin_note,omitempty"`
	RefundAmount   *int64     `json:"refund_amount,omitempty"`
	RefundReason   *string    `json:"refund_reason,omitempty"`
	RefundEvidenceURLs []string `json:"refund_evidence_urls,omitempty"`
	RefundedAt     *time.Time `json:"refunded_at,omitempty"`
	RefundBankRef  *string    `json:"refund_bank_ref,omitempty"`
	IsTopUp        bool       `json:"is_top_up"`
	ParentPaymentID *int64    `json:"parent_payment_id,omitempty"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ImportSuspenseResponseDTO reports the batch import outcome.
type ImportSuspenseResponseDTO struct {
	TotalProcessed   int    `json:"total_processed"`
	ImportedCount    int    `json:"imported_count"`
	SkippedCount     int    `json:"skipped_count"`
	AutoMatchedCount int    `json:"auto_matched_count"`
	Message          string `json:"message"`
}

// EvidenceUploadSignatureResponseDTO provides the signature for uploading evidence images to Cloudinary.
type EvidenceUploadSignatureResponseDTO struct {
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	ApiKey    string `json:"api_key"`
	CloudName string `json:"cloud_name"`
	Folder    string `json:"folder"`
}

// TopUpPaymentResponseDTO holds top-up link creation result.
type TopUpPaymentResponseDTO struct {
	TopUpPaymentID int64      `json:"top_up_payment_id"`
	ParentPaymentID int64     `json:"parent_payment_id"`
	AdditionalAmount int64    `json:"additional_amount"`
	CheckoutURL    string     `json:"checkout_url"`
	SeatHeldUntil  *time.Time `json:"seat_held_until,omitempty"`
	Message        string     `json:"message"`
}

// ResolveOverpaymentResponseDTO holds the result of confirming seat and issuing partial refund.
type ResolveOverpaymentResponseDTO struct {
	ConfirmedPayment AdminPaymentResponseDTO `json:"confirmed_payment"`
	RefundPayment    AdminPaymentResponseDTO `json:"refund_payment"`
	Message          string                  `json:"message"`
}

// DoubleChargePaymentDTO represents a payment item inside a double-charge alert group.
type DoubleChargePaymentDTO struct {
	PaymentID      int64      `json:"payment_id"`
	RegistrationID int64      `json:"exam_registration_id"`
	Amount         int64      `json:"amount"`
	AmountReceived *int64     `json:"amount_received,omitempty"`
	Status         string     `json:"status"`
	TransactionRef *string    `json:"transaction_ref,omitempty"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// DoubleChargeGroupResponseDTO aggregates transactions belonging to the same candidate & session.
type DoubleChargeGroupResponseDTO struct {
	CandidateID       int64                    `json:"candidate_id"`
	CandidateName     string                   `json:"candidate_name"`
	CandidateCCCD     string                   `json:"candidate_cccd"`
	ExamSessionID     int64                    `json:"exam_session_id"`
	ExamTypeName      string                   `json:"exam_type_name"`
	ExamSessionDate   string                   `json:"exam_session_date"`
	SuspectedPayments []DoubleChargePaymentDTO `json:"suspected_payments"`
}

// ResolveDoubleChargeResponseDTO summarizes resolving a double charge case.
type ResolveDoubleChargeResponseDTO struct {
	KeptPayment     AdminPaymentResponseDTO   `json:"kept_payment"`
	RefundedPayments []AdminPaymentResponseDTO `json:"refunded_payments"`
	Message          string                    `json:"message"`
}

// SuspenseTransactionResponseDTO mirrors payment_suspense_transactions for admin views.
type SuspenseTransactionResponseDTO struct {
	ID                   int64      `json:"id"`
	OrderCode            *string    `json:"order_code,omitempty"`
	Amount               int64      `json:"amount"`
	BankRef              *string    `json:"bank_ref,omitempty"`
	TransferContent      *string    `json:"transfer_content,omitempty"`
	TransferTime         *time.Time `json:"transfer_time,omitempty"`
	EvidenceURLs         []string   `json:"evidence_urls"`
	Status               string     `json:"status"`
	MatchedRegistrationID *int64   `json:"matched_registration_id,omitempty"`
	MatchedPaymentID     *int64     `json:"matched_payment_id,omitempty"`
	MatchedByAdminID     *int64     `json:"matched_by_admin_id,omitempty"`
	MatchedAt            *time.Time `json:"matched_at,omitempty"`
	AdminNote            *string    `json:"admin_note,omitempty"`
	Source               string     `json:"source"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// MatchSuspenseResponseDTO holds the result of mapping a suspense transaction to a registration.
type MatchSuspenseResponseDTO struct {
	SuspenseTransaction SuspenseTransactionResponseDTO `json:"suspense_transaction"`
	ConfirmedPayment    AdminPaymentResponseDTO        `json:"confirmed_payment"`
	Message             string                         `json:"message"`
}

