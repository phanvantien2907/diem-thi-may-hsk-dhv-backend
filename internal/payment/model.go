package payment

import (
	"time"

	"github.com/hskvinh/backend/internal/payment/enum"
)

// PaymentEntity mirrors the payments table 1:1.
type PaymentEntity struct {
	PaymentID                 int64
	PaymentExamRegistrationID int64
	PaymentAmount             int64
	PaymentAmountReceived     *int64
	PaymentMethod             enum.PaymentMethod
	PaymentGateway            *string
	PaymentTransactionRef     *string
	PaymentGatewayTransID     *string
	PaymentContent            *string
	PaymentStatus             enum.PaymentStatus
	PaymentExpiresAt          *time.Time
	PaymentPaidAt             *time.Time
	PaymentIsDeleted          bool
	PaymentDeletedAt          *time.Time
	PaymentCreatedAt          time.Time
	PaymentUpdatedAt          time.Time
}
