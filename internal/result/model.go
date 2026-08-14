package result

import "time"

// ExamResultEntity mirrors the exam_results table 1:1.
type ExamResultEntity struct {
	ExamResultID                   int64
	ExamResultExamRegistrationID   int64
	ExamResultListeningScore        *float64
	ExamResultReadingScore         *float64
	ExamResultWritingScore         *float64
	ExamResultTotalScore           *float64
	ExamResultStatus               *ExamResultStatus
	ExamResultPublishedAt          *time.Time
	ExamResultIsDeleted            bool
	ExamResultDeletedAt            *time.Time
	ExamResultCreatedAt            time.Time
	ExamResultUpdatedAt            time.Time
}

// ExamRecheckRequestEntity mirrors the exam_recheck_requests table 1:1.
type ExamRecheckRequestEntity struct {
	ExamRecheckID                     int64
	ExamRecheckExamResultID           int64
	ExamRecheckReason                 string
	ExamRecheckStatus                 RecheckStatus
	ExamRecheckReviewedByAccountID    *int64
	ExamRecheckReviewedAt             *time.Time
	ExamRecheckNewTotalScore          *float64
	ExamRecheckIsDeleted              bool
	ExamRecheckDeletedAt              *time.Time
	ExamRecheckCreatedAt              time.Time
	ExamRecheckUpdatedAt              time.Time
}

// ── Internal service inputs ───────────────────────────────────────────────────

// CreateResultInput carries validated data from CreateResultRequestDTO.
type CreateResultInput struct {
	RegistrationID int64
	Listening      float64
	Reading        float64
	Writing        float64
	Total          float64
	Status         ExamResultStatus
}

// ReviewRecheckInput carries validated reviewer decision.
type ReviewRecheckInput struct {
	RecheckID     int64
	Status        RecheckStatus
	ReviewerID    int64
	NewTotalScore *float64
}
