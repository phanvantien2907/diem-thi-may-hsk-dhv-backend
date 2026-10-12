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
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
