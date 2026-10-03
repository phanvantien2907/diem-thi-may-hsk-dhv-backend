package enum

// ExceptionType classifies the kind of transaction anomaly.
type ExceptionType string

const (
	ExceptionAmountMismatch ExceptionType = "amount_mismatch"
	ExceptionWebhookFailed  ExceptionType = "webhook_failed"
	ExceptionExpiredPayment ExceptionType = "expired_payment"
)

// Valid reports whether t is a recognised ExceptionType.
func (t ExceptionType) Valid() bool {
	switch t {
	case ExceptionAmountMismatch, ExceptionWebhookFailed, ExceptionExpiredPayment:
		return true
	}
	return false
}

// String returns the string representation.
func (t ExceptionType) String() string { return string(t) }

// RefundReason classifies why a refund is needed.
type RefundReason string

const (
	RefundOverpayment RefundReason = "overpayment"
	RefundSlotExpired RefundReason = "slot_expired"
	RefundOther       RefundReason = "other"
)

// Valid reports whether r is a recognised RefundReason.
func (r RefundReason) Valid() bool {
	switch r {
	case RefundOverpayment, RefundSlotExpired, RefundOther:
		return true
	}
	return false
}

// String returns the string representation.
func (r RefundReason) String() string { return string(r) }
