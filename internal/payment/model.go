package payment

import "time"

// PaymentMethod enumerates supported payment gateways.
type PaymentMethod string

const (
	PayMethodVNPay        PaymentMethod = "vnpay"
	PayMethodMomo         PaymentMethod = "momo"
	PayMethodZalopay      PaymentMethod = "zalopay"
	PayMethodBankTransfer PaymentMethod = "bank_transfer"
	PayMethodCash         PaymentMethod = "cash"
)

// PaymentStatus enumerates payment lifecycle states.
type PaymentStatus string

const (
	PayStatusPending  PaymentStatus = "pending"
	PayStatusSuccess  PaymentStatus = "success"
	PayStatusFailed   PaymentStatus = "failed"
	PayStatusRefunded PaymentStatus = "refunded"
)

// Payment maps to the payments table.
// payment_amount is NUMERIC(12,0) stored as int64 (VND, no decimals).
type Payment struct {
	PaymentID                 int64         `json:"payment_id"`
	PaymentExamRegistrationID int64         `json:"payment_exam_registration_id"`
	PaymentAmount             int64         `json:"payment_amount"`
	PaymentMethod             PaymentMethod `json:"payment_method"`
	PaymentTransactionRef     *string       `json:"payment_transaction_ref,omitempty"`
	PaymentStatus             PaymentStatus `json:"payment_status"`
	PaymentPaidAt             *time.Time    `json:"payment_paid_at,omitempty"`
	PaymentIsDeleted          bool          `json:"payment_is_deleted"`
	PaymentDeletedAt          *time.Time    `json:"payment_deleted_at,omitempty"`
	PaymentCreatedAt          time.Time     `json:"payment_created_at"`
	PaymentUpdatedAt          time.Time     `json:"payment_updated_at"`
}

// CreatePaymentInput holds fields required to open a payment.
type CreatePaymentInput struct {
	RegistrationID int64
	Amount         int64
	Method         PaymentMethod
}
