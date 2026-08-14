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
	PaymentMethod             enum.PaymentMethod
	PaymentTransactionRef     *string
	PaymentStatus             enum.PaymentStatus
	PaymentPaidAt             *time.Time
	PaymentIsDeleted          bool
	PaymentDeletedAt          *time.Time
	PaymentCreatedAt          time.Time
	PaymentUpdatedAt          time.Time
}
