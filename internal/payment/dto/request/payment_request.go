package request

// CreatePaymentRequestDTO is the payload for POST /payments/payos/create.
// The candidate provides only the registration ID — amount is looked up server-side.
type CreatePaymentRequestDTO struct {
	ExamRegistrationID int64  `json:"exam_registration_id" validate:"required,min=1"`
	ReturnURL          string `json:"return_url" validate:"required,url"`
	CancelURL          string `json:"cancel_url" validate:"required,url"`
}

// --- PayOS Webhook Structures ---

// PayOSWebhookData mirrors the "data" object PayOS sends in webhooks.
type PayOSWebhookData struct {
	OrderCode              int64  `json:"orderCode"`
	Amount                 int64  `json:"amount"`
	Description            string `json:"description"`
	AccountNumber          string `json:"accountNumber"`
	Reference              string `json:"reference"`
	TransactionDateTime    string `json:"transactionDateTime"`
	Currency               string `json:"currency"`
	PaymentLinkID          string `json:"paymentLinkId"`
	Code                   string `json:"code"`
	Desc                   string `json:"desc"`
	CounterAccountBankID   string `json:"counterAccountBankId"`
	CounterAccountBankName string `json:"counterAccountBankName"`
	CounterAccountName     string `json:"counterAccountName"`
	CounterAccountNumber   string `json:"counterAccountNumber"`
	VirtualAccountName     string `json:"virtualAccountName"`
	VirtualAccountNumber   string `json:"virtualAccountNumber"`
}

// PayOSWebhookPayload is the top-level JSON envelope PayOS POSTs to our callback.
type PayOSWebhookPayload struct {
	Code      string           `json:"code"`
	Desc      string           `json:"desc"`
	Success   bool             `json:"success"`
	Data      PayOSWebhookData `json:"data"`
	Signature string           `json:"signature"`
}

// --- Admin Request DTOs ---

// AdminListPaymentsRequestDTO carries query parameters for the admin payment list.
type AdminListPaymentsRequestDTO struct {
	Page          int    `form:"page"`
	Limit         int    `form:"limit"`
	Status        string `form:"status"`
	From          string `form:"from"`
	To            string `form:"to"`
	OrderCode     string `form:"order_code"`
	TransactionID string `form:"transaction_id"`
}

// AdminConfirmPaymentRequestDTO requires evidence when manually confirming a payment.
type AdminConfirmPaymentRequestDTO struct {
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Note         string   `json:"note"`
}

// AdminCancelPaymentRequestDTO requires evidence when cancelling a payment.
type AdminCancelPaymentRequestDTO struct {
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Reason       string   `json:"reason" validate:"required,min=3"`
}

// AdminMarkRefundRequestDTO requires evidence when marking a payment for refund.
type AdminMarkRefundRequestDTO struct {
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Reason       string   `json:"reason" validate:"required,min=3"`
}

// AdminTopUpPaymentRequestDTO is the payload for creating a top-up payment link.
type AdminTopUpPaymentRequestDTO struct {
	AdditionalAmount *int64   `json:"additional_amount" validate:"omitempty,min=1000"`
	ExtendHours      int      `json:"extend_hours" validate:"omitempty,min=1,max=168"`
	EvidenceURLs     []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Note             string   `json:"note"`
	ReturnURL        string   `json:"return_url" validate:"omitempty,url"`
	CancelURL        string   `json:"cancel_url" validate:"omitempty,url"`
}

// AdminResolveOverpaymentRequestDTO is the payload for confirming registration and issuing a partial refund.
type AdminResolveOverpaymentRequestDTO struct {
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	RefundAmount *int64   `json:"refund_amount" validate:"omitempty,min=1000"`
	Note         string   `json:"note" validate:"required,min=3"`
}

// AdminResolveDoubleChargeRequestDTO is the payload for resolving a double charge case.
type AdminResolveDoubleChargeRequestDTO struct {
	KeepPaymentID    int64    `json:"keep_payment_id" validate:"required,min=1"`
	RefundPaymentIDs []int64  `json:"refund_payment_ids" validate:"required,min=1,dive,min=1"`
	EvidenceURLs     []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Note             string   `json:"note" validate:"required,min=3"`
}

// AdminCreateSuspenseRequestDTO is the payload for adding an unmatched bank transfer record.
type AdminCreateSuspenseRequestDTO struct {
	Amount          int64    `json:"amount" validate:"required,min=1000"`
	BankRef         string   `json:"bank_ref" validate:"required"`
	TransferContent string   `json:"transfer_content" validate:"required"`
	TransferTime    *string  `json:"transfer_time"`
	EvidenceURLs    []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	AdminNote       string   `json:"admin_note"`
}

// AdminMatchSuspenseRequestDTO is the payload for manually mapping a suspense transaction to an exam registration.
type AdminMatchSuspenseRequestDTO struct {
	ExamRegistrationID int64    `json:"exam_registration_id" validate:"required,min=1"`
	EvidenceURLs       []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	Note               string   `json:"note"`
}

// AdminListSuspenseRequestDTO carries query parameters for suspense transactions.
type AdminListSuspenseRequestDTO struct {
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
	Status string `form:"status"`
	From   string `form:"from"`
	To     string `form:"to"`
	Search string `form:"search"`
}

// AdminConfirmRefundRequestDTO is the payload for confirming that a refund has been transferred to candidate.
type AdminConfirmRefundRequestDTO struct {
	EvidenceURLs []string `json:"evidence_urls" validate:"required,min=1,dive,url"`
	BankRef      string   `json:"bank_ref" validate:"required,min=2"`
	Note         string   `json:"note"`
}

// AdminImportSuspenseItemDTO represents a single row in an imported statement.
type AdminImportSuspenseItemDTO struct {
	BankRef         string  `json:"bank_ref" validate:"required"`
	Amount          int64   `json:"amount" validate:"required,min=1000"`
	TransferContent string  `json:"transfer_content" validate:"required"`
	TransferTime    *string `json:"transfer_time"`
}

// AdminImportSuspenseRequestDTO is the payload when importing statements.
type AdminImportSuspenseRequestDTO struct {
	EvidenceURLs []string                     `json:"evidence_urls"`
	Items        []AdminImportSuspenseItemDTO `json:"items" validate:"required,min=1,dive"`
}


