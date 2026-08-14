package payment

// PaymentMethod enumerates supported payment gateways.
type PaymentMethod string

const (
	PayMethodVNPay        PaymentMethod = "vnpay"
	PayMethodMomo         PaymentMethod = "momo"
	PayMethodZalopay      PaymentMethod = "zalopay"
	PayMethodBankTransfer PaymentMethod = "bank_transfer"
	PayMethodCash         PaymentMethod = "cash"
)

func (m PaymentMethod) Valid() bool {
	switch m {
	case PayMethodVNPay, PayMethodMomo, PayMethodZalopay, PayMethodBankTransfer, PayMethodCash:
		return true
	}
	return false
}

// PaymentStatus enumerates payment lifecycle states.
type PaymentStatus string

const (
	PayStatusPending  PaymentStatus = "pending"
	PayStatusSuccess  PaymentStatus = "success"
	PayStatusFailed   PaymentStatus = "failed"
	PayStatusRefunded PaymentStatus = "refunded"
)

func (s PaymentStatus) Valid() bool {
	switch s {
	case PayStatusPending, PayStatusSuccess, PayStatusFailed, PayStatusRefunded:
		return true
	}
	return false
}
