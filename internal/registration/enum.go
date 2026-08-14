package registration

// ExamRegistrationStatus enumerates registration lifecycle states.
type ExamRegistrationStatus string

const (
	RegStatusPendingPayment ExamRegistrationStatus = "pending_payment"
	RegStatusConfirmed      ExamRegistrationStatus = "confirmed"
	RegStatusCancelled      ExamRegistrationStatus = "cancelled"
)

func (s ExamRegistrationStatus) Valid() bool {
	switch s {
	case RegStatusPendingPayment, RegStatusConfirmed, RegStatusCancelled:
		return true
	}
	return false
}
