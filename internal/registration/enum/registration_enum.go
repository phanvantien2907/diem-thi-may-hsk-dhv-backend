package enum

// ExamRegistrationStatus enumerates registration lifecycle states.
type ExamRegistrationStatus string

const (
	RegStatusPendingPayment ExamRegistrationStatus = "pending_payment"
	RegStatusConfirmed      ExamRegistrationStatus = "confirmed"
	RegStatusCancelled      ExamRegistrationStatus = "cancelled"
)

// Valid reports whether s is a recognised ExamRegistrationStatus.
func (s ExamRegistrationStatus) Valid() bool {
	switch s {
	case RegStatusPendingPayment, RegStatusConfirmed, RegStatusCancelled:
		return true
	}
	return false
}

// String returns the string representation.
func (s ExamRegistrationStatus) String() string { return string(s) }
