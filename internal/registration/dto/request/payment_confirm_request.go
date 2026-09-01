package request

// PaymentConfirmRequestDTO is the payload for POST /me/registrations/:id/payment-confirm.
type PaymentConfirmRequestDTO struct {
	Amount         float64 `json:"amount"          validate:"required,gt=0"`
	Method         string  `json:"method"          validate:"required"`
	TransactionRef string  `json:"transaction_ref" validate:"required"`
}
