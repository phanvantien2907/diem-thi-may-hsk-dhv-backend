package result

import "time"

// ── ExamResult ────────────────────────────────────────────────────────────

// ExamResultStatus enumerates pass/fail outcomes.
type ExamResultStatus string

const (
	ResultStatusPass ExamResultStatus = "pass"
	ResultStatusFail ExamResultStatus = "fail"
)

// ExamResult maps to the exam_results table.
// Score columns are NUMERIC(5,1)/NUMERIC(6,1) → stored as *float64.
type ExamResult struct {
	ExamResultID                 int64             `json:"exam_result_id"`
	ExamResultExamRegistrationID int64             `json:"exam_result_exam_registration_id"`
	ExamResultListeningScore     *float64          `json:"exam_result_listening_score,omitempty"`
	ExamResultReadingScore       *float64          `json:"exam_result_reading_score,omitempty"`
	ExamResultWritingScore       *float64          `json:"exam_result_writing_score,omitempty"`
	ExamResultTotalScore         *float64          `json:"exam_result_total_score,omitempty"`
	ExamResultStatus             *ExamResultStatus `json:"exam_result_status,omitempty"`
	ExamResultPublishedAt        *time.Time        `json:"exam_result_published_at,omitempty"`
	ExamResultIsDeleted          bool              `json:"exam_result_is_deleted"`
	ExamResultDeletedAt          *time.Time        `json:"exam_result_deleted_at,omitempty"`
	ExamResultCreatedAt          time.Time         `json:"exam_result_created_at"`
	ExamResultUpdatedAt          time.Time         `json:"exam_result_updated_at"`
}

// ── ExamRecheckRequest ────────────────────────────────────────────────────

// RecheckStatus enumerates recheck request lifecycle states.
type RecheckStatus string

const (
	RecheckSubmitted RecheckStatus = "submitted"
	RecheckInReview  RecheckStatus = "in_review"
	RecheckCompleted RecheckStatus = "completed"
	RecheckRejected  RecheckStatus = "rejected"
)

// ExamRecheckRequest maps to the exam_recheck_requests table.
type ExamRecheckRequest struct {
	ExamRecheckID                  int64         `json:"exam_recheck_id"`
	ExamRecheckExamResultID        int64         `json:"exam_recheck_exam_result_id"`
	ExamRecheckReason              string        `json:"exam_recheck_reason"`
	ExamRecheckStatus              RecheckStatus `json:"exam_recheck_status"`
	ExamRecheckReviewedByAccountID *int64        `json:"exam_recheck_reviewed_by_account_id,omitempty"`
	ExamRecheckReviewedAt          *time.Time    `json:"exam_recheck_reviewed_at,omitempty"`
	ExamRecheckNewTotalScore       *float64      `json:"exam_recheck_new_total_score,omitempty"`
	ExamRecheckIsDeleted           bool          `json:"exam_recheck_is_deleted"`
	ExamRecheckDeletedAt           *time.Time    `json:"exam_recheck_deleted_at,omitempty"`
	ExamRecheckCreatedAt           time.Time     `json:"exam_recheck_created_at"`
	ExamRecheckUpdatedAt           time.Time     `json:"exam_recheck_updated_at"`
}

// ── DTOs ──────────────────────────────────────────────────────────────────

// CreateResultInput holds score data from admin.
type CreateResultInput struct {
	RegistrationID int64
	Listening      float64
	Reading        float64
	Writing        float64
	Total          float64
	Status         ExamResultStatus
}

// ReviewRecheckInput holds a reviewer's decision.
type ReviewRecheckInput struct {
	RecheckID     int64
	ReviewerID    int64
	NewTotalScore *float64
	Status        RecheckStatus
}
