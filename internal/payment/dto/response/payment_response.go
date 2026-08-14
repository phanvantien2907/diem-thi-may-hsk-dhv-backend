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
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
