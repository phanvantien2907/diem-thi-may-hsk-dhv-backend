package result

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles exam results and recheck requests.
type Service struct{ db *sql.DB }

// NewService creates a result Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateResult inserts an exam result (admin).
func (s *Service) CreateResult(ctx context.Context, in CreateResultInput) (*ExamResult, error) {
	var r ExamResult
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO exam_results
		       (exam_result_exam_registration_id,
		        exam_result_listening_score, exam_result_reading_score,
		        exam_result_writing_score, exam_result_total_score,
		        exam_result_status, exam_result_published_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		RETURNING exam_result_id, exam_result_exam_registration_id,
		          exam_result_listening_score, exam_result_reading_score,
		          exam_result_writing_score, exam_result_total_score,
		          exam_result_status, exam_result_published_at,
		          exam_result_is_deleted, exam_result_created_at, exam_result_updated_at`,
		in.RegistrationID, in.Listening, in.Reading, in.Writing, in.Total, in.Status,
	).Scan(
		&r.ExamResultID, &r.ExamResultExamRegistrationID,
		&r.ExamResultListeningScore, &r.ExamResultReadingScore,
		&r.ExamResultWritingScore, &r.ExamResultTotalScore,
		&r.ExamResultStatus, &r.ExamResultPublishedAt,
		&r.ExamResultIsDeleted, &r.ExamResultCreatedAt, &r.ExamResultUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("result: create: %w", err)
	}
	return &r, nil
}

// GetResultByRegistration returns the result for a given registration ID.
func (s *Service) GetResultByRegistration(ctx context.Context, registrationID int64) (*ExamResult, error) {
	var r ExamResult
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_result_id, exam_result_exam_registration_id,
		       exam_result_listening_score, exam_result_reading_score,
		       exam_result_writing_score, exam_result_total_score,
		       exam_result_status, exam_result_published_at,
		       exam_result_is_deleted, exam_result_created_at, exam_result_updated_at
		FROM exam_results
		WHERE exam_result_exam_registration_id = $1 AND exam_result_is_deleted = FALSE`,
		registrationID,
	).Scan(
		&r.ExamResultID, &r.ExamResultExamRegistrationID,
		&r.ExamResultListeningScore, &r.ExamResultReadingScore,
		&r.ExamResultWritingScore, &r.ExamResultTotalScore,
		&r.ExamResultStatus, &r.ExamResultPublishedAt,
		&r.ExamResultIsDeleted, &r.ExamResultCreatedAt, &r.ExamResultUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("result not found")
	}
	if err != nil {
		return nil, fmt.Errorf("result: get: %w", err)
	}
	return &r, nil
}

// CreateRecheckRequest submits a recheck request.
func (s *Service) CreateRecheckRequest(ctx context.Context, resultID int64, reason string) (*ExamRecheckRequest, error) {
	var r ExamRecheckRequest
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO exam_recheck_requests
		       (exam_recheck_exam_result_id, exam_recheck_reason, exam_recheck_status)
		VALUES ($1,$2,'submitted')
		RETURNING exam_recheck_id, exam_recheck_exam_result_id, exam_recheck_reason,
		          exam_recheck_status, exam_recheck_reviewed_by_account_id,
		          exam_recheck_reviewed_at, exam_recheck_new_total_score,
		          exam_recheck_is_deleted, exam_recheck_created_at, exam_recheck_updated_at`,
		resultID, reason,
	).Scan(
		&r.ExamRecheckID, &r.ExamRecheckExamResultID, &r.ExamRecheckReason,
		&r.ExamRecheckStatus, &r.ExamRecheckReviewedByAccountID,
		&r.ExamRecheckReviewedAt, &r.ExamRecheckNewTotalScore,
		&r.ExamRecheckIsDeleted, &r.ExamRecheckCreatedAt, &r.ExamRecheckUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("result: create recheck: %w", err)
	}
	return &r, nil
}

// ReviewRecheckRequest records a reviewer's decision.
func (s *Service) ReviewRecheckRequest(ctx context.Context, in ReviewRecheckInput) (*ExamRecheckRequest, error) {
	var r ExamRecheckRequest
	err := s.db.QueryRowContext(ctx, `
		UPDATE exam_recheck_requests
		SET exam_recheck_status                 = $1,
		    exam_recheck_reviewed_by_account_id = $2,
		    exam_recheck_reviewed_at            = NOW(),
		    exam_recheck_new_total_score        = $3,
		    exam_recheck_updated_at             = NOW()
		WHERE exam_recheck_id = $4 AND exam_recheck_is_deleted = FALSE
		RETURNING exam_recheck_id, exam_recheck_exam_result_id, exam_recheck_reason,
		          exam_recheck_status, exam_recheck_reviewed_by_account_id,
		          exam_recheck_reviewed_at, exam_recheck_new_total_score,
		          exam_recheck_is_deleted, exam_recheck_created_at, exam_recheck_updated_at`,
		in.Status, in.ReviewerID, in.NewTotalScore, in.RecheckID,
	).Scan(
		&r.ExamRecheckID, &r.ExamRecheckExamResultID, &r.ExamRecheckReason,
		&r.ExamRecheckStatus, &r.ExamRecheckReviewedByAccountID,
		&r.ExamRecheckReviewedAt, &r.ExamRecheckNewTotalScore,
		&r.ExamRecheckIsDeleted, &r.ExamRecheckCreatedAt, &r.ExamRecheckUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("recheck request not found")
	}
	if err != nil {
		return nil, fmt.Errorf("result: review recheck: %w", err)
	}
	return &r, nil
}
