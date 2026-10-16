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
	PaymentEvidenceURLs       []string
	PaymentAdminNote          *string
	PaymentRefundAmount       *int64
	PaymentRefundReason       *string
	PaymentRefundEvidenceURLs []string
	PaymentRefundedAt         *time.Time
	PaymentRefundBankRef      *string
	PaymentIsTopUp            bool
	PaymentParentPaymentID    *int64
	PaymentExpiresAt          *time.Time
	PaymentPaidAt             *time.Time
	PaymentIsDeleted          bool
	PaymentDeletedAt          *time.Time
	PaymentCreatedAt          time.Time
	PaymentUpdatedAt          time.Time
}

// SuspenseTransactionEntity mirrors payment_suspense_transactions table 1:1.
type SuspenseTransactionEntity struct {
	SuspenseID                    int64
	SuspenseOrderCode             *string
	SuspenseAmount                int64
	SuspenseBankRef               *string
	SuspenseTransferContent       *string
	SuspenseTransferTime          *time.Time
	SuspenseEvidenceURLs          []string
	SuspenseStatus                string
	SuspenseMatchedRegistrationID *int64
	SuspenseMatchedPaymentID      *int64
	SuspenseMatchedByAdminID      *int64
	SuspenseMatchedAt             *time.Time
	SuspenseAdminNote             *string
	SuspenseSource                string
	SuspenseRawPayload            []byte
	SuspenseIsDeleted             bool
	SuspenseDeletedAt             *time.Time
	SuspenseCreatedAt             time.Time
	SuspenseUpdatedAt             time.Time
}

