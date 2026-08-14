package payment

import "time"

// PaymentEntity mirrors the payments table 1:1.
type PaymentEntity struct {
	PaymentID                   int64
	PaymentExamRegistrationID   int64
	PaymentAmount               int64
	PaymentMethod               PaymentMethod
	PaymentTransactionRef       *string
	PaymentStatus               PaymentStatus
	PaymentPaidAt               *time.Time
	PaymentIsDeleted            bool
	PaymentDeletedAt            *time.Time
	PaymentCreatedAt            time.Time
	PaymentUpdatedAt            time.Time
}

// ── Internal service inputs ───────────────────────────────────────────────────

// CreatePaymentInput carries validated data from CreatePaymentRequestDTO.
type CreatePaymentInput struct {
	RegistrationID int64
	Amount         int64
	Method         PaymentMethod
}
