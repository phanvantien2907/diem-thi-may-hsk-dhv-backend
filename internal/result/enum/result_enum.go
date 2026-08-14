package enum

// ExamResultStatus enumerates pass/fail outcomes.
type ExamResultStatus string

const (
	ResultStatusPass ExamResultStatus = "pass"
	ResultStatusFail ExamResultStatus = "fail"
)

// Valid reports whether s is a recognised ExamResultStatus.
func (s ExamResultStatus) Valid() bool {
	return s == ResultStatusPass || s == ResultStatusFail
}

// String returns the string representation.
func (s ExamResultStatus) String() string { return string(s) }

// RecheckStatus enumerates recheck request lifecycle states.
type RecheckStatus string

const (
	RecheckSubmitted RecheckStatus = "submitted"
	RecheckInReview  RecheckStatus = "in_review"
	RecheckCompleted RecheckStatus = "completed"
	RecheckRejected  RecheckStatus = "rejected"
)

// Valid reports whether s is a recognised RecheckStatus.
func (s RecheckStatus) Valid() bool {
	switch s {
	case RecheckSubmitted, RecheckInReview, RecheckCompleted, RecheckRejected:
		return true
	}
	return false
}

// String returns the string representation.
func (s RecheckStatus) String() string { return string(s) }
