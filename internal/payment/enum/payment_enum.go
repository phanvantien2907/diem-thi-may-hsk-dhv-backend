package enum

// PaymentMethod enumerates supported payment gateways.
type PaymentMethod string

const (
	PayMethodVNPay        PaymentMethod = "vnpay"
	PayMethodMomo         PaymentMethod = "momo"
	PayMethodZalopay      PaymentMethod = "zalopay"
	PayMethodBankTransfer PaymentMethod = "bank_transfer"
	PayMethodCash         PaymentMethod = "cash"
	PayMethodPayOS        PaymentMethod = "payos"
)

// Valid reports whether m is a recognised PaymentMethod.
func (m PaymentMethod) Valid() bool {
	switch m {
	case PayMethodVNPay, PayMethodMomo, PayMethodZalopay, PayMethodBankTransfer, PayMethodCash, PayMethodPayOS:
		return true
	}
	return false
}

// String returns the string representation.
func (m PaymentMethod) String() string { return string(m) }

// PaymentStatus enumerates payment lifecycle states.
type PaymentStatus string

const (
	PayStatusPending        PaymentStatus = "pending"
	PayStatusSuccess        PaymentStatus = "success"
	PayStatusFailed         PaymentStatus = "failed"
	PayStatusRefunded       PaymentStatus = "refunded"
	PayStatusPendingRefund  PaymentStatus = "pending_refund"
	PayStatusExpired        PaymentStatus = "expired"
	PayStatusCancelled      PaymentStatus = "cancelled"
	PayStatusAmountMismatch PaymentStatus = "amount_mismatch"
	PayStatusPartialPaid    PaymentStatus = "partial_paid"
)

// Valid reports whether s is a recognised PaymentStatus.
func (s PaymentStatus) Valid() bool {
	switch s {
	case PayStatusPending, PayStatusSuccess, PayStatusFailed, PayStatusRefunded, PayStatusPendingRefund,
		PayStatusExpired, PayStatusCancelled, PayStatusAmountMismatch, PayStatusPartialPaid:
		return true
	}
	return false
}

// String returns the string representation.
func (s PaymentStatus) String() string { return string(s) }
