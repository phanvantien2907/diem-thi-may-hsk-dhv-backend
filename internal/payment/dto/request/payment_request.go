package request

// CreatePaymentRequestDTO is the payload for POST /payments.
type CreatePaymentRequestDTO struct {
	ExamRegistrationID int64  `json:"exam_registration_id" validate:"required,min=1"`
	Amount             int64  `json:"amount"               validate:"required,min=1"`
	Method             string `json:"method"               validate:"required,oneof=vnpay momo zalopay bank_transfer cash"`
}

// PaymentCallbackRequestDTO is the payload for POST /payments/callback.
type PaymentCallbackRequestDTO struct {
	TransactionRef string `json:"transaction_ref" validate:"required"`
	Status         string `json:"status"          validate:"required,oneof=pending success failed refunded"`
}
