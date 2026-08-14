package result

// ExamResultStatus enumerates pass/fail outcomes.
type ExamResultStatus string

const (
	ResultStatusPass ExamResultStatus = "pass"
	ResultStatusFail ExamResultStatus = "fail"
)

func (s ExamResultStatus) Valid() bool {
	return s == ResultStatusPass || s == ResultStatusFail
}

// RecheckStatus enumerates recheck request lifecycle states.
type RecheckStatus string

const (
	RecheckSubmitted RecheckStatus = "submitted"
	RecheckInReview  RecheckStatus = "in_review"
	RecheckCompleted RecheckStatus = "completed"
	RecheckRejected  RecheckStatus = "rejected"
)

func (s RecheckStatus) Valid() bool {
	switch s {
	case RecheckSubmitted, RecheckInReview, RecheckCompleted, RecheckRejected:
		return true
	}
	return false
}
