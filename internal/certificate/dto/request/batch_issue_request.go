package request

// BatchIssueRequestDTO is the admin payload for POST /admin/certificates/issue-batch.
type BatchIssueRequestDTO struct {
	ExamSessionID int64  `json:"exam_session_id" validate:"required,min=1"`
	IssueDate     string `json:"issue_date"      validate:"required"` // YYYY-MM-DD
}
