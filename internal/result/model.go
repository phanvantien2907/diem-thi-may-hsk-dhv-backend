package result

import (
	"time"

	"github.com/hskvinh/backend/internal/result/enum"
)

// ExamResultEntity mirrors the exam_results table 1:1.
type ExamResultEntity struct {
	ExamResultID                 int64
	ExamResultExamRegistrationID int64
	ExamResultListeningScore     *float64
	ExamResultReadingScore       *float64
	ExamResultWritingScore       *float64
	ExamResultTotalScore         *float64
	ExamResultStatus             *enum.ExamResultStatus
	ExamResultPublishedAt        *time.Time
	ExamResultIsDeleted          bool
	ExamResultDeletedAt          *time.Time
	ExamResultCreatedAt          time.Time
	ExamResultUpdatedAt          time.Time
}

// ExamRecheckRequestEntity mirrors the exam_recheck_requests table 1:1.
type ExamRecheckRequestEntity struct {
	ExamRecheckID                  int64
	ExamRecheckExamResultID        int64
	ExamRecheckReason              string
	ExamRecheckStatus              enum.RecheckStatus
	ExamRecheckReviewedByAccountID *int64
	ExamRecheckReviewedAt          *time.Time
	ExamRecheckNewTotalScore       *float64
	ExamRecheckIsDeleted           bool
	ExamRecheckDeletedAt           *time.Time
	ExamRecheckCreatedAt           time.Time
	ExamRecheckUpdatedAt           time.Time
}

// ── Internal service inputs ───────────────────────────────────────────────────

// ReviewRecheckInput carries validated reviewer decision.
type ReviewRecheckInput struct {
	RecheckID     int64
	Status        enum.RecheckStatus
	ReviewerID    int64
	NewTotalScore *float64
}
