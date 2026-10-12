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
