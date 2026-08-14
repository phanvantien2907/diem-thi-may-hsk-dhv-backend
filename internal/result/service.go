package result

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hskvinh/backend/internal/result/dto/request"
	"github.com/hskvinh/backend/internal/result/dto/response"
	"github.com/hskvinh/backend/internal/result/enum"
)

// Service handles exam results and recheck requests.
type Service struct{ db *sql.DB }

// NewService creates a result Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateResult inserts an exam result and returns its public DTO.
func (s *Service) CreateResult(ctx context.Context, req request.CreateResultRequestDTO) (response.ExamResultResponseDTO, error) {
	if !enum.ExamResultStatus(req.Status).Valid() {
		return response.ExamResultResponseDTO{}, fmt.Errorf("invalid result status: %s", req.Status)
	}
	var e ExamResultEntity
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
		          exam_result_is_deleted, exam_result_deleted_at,
		          exam_result_created_at, exam_result_updated_at`,
		req.ExamRegistrationID, req.ListeningScore, req.ReadingScore,
		req.WritingScore, req.TotalScore, req.Status,
	).Scan(
		&e.ExamResultID, &e.ExamResultExamRegistrationID,
		&e.ExamResultListeningScore, &e.ExamResultReadingScore,
		&e.ExamResultWritingScore, &e.ExamResultTotalScore,
		&e.ExamResultStatus, &e.ExamResultPublishedAt,
		&e.ExamResultIsDeleted, &e.ExamResultDeletedAt,
		&e.ExamResultCreatedAt, &e.ExamResultUpdatedAt,
	)
	if err != nil {
		return response.ExamResultResponseDTO{}, fmt.Errorf("result: create: %w", err)
	}
	return ToExamResultResponseDTO(e), nil
}

// GetResultByRegistration returns the result DTO for a given registration ID.
func (s *Service) GetResultByRegistration(ctx context.Context, registrationID int64) (response.ExamResultResponseDTO, error) {
	var e ExamResultEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_result_id, exam_result_exam_registration_id,
		       exam_result_listening_score, exam_result_reading_score,
		       exam_result_writing_score, exam_result_total_score,
		       exam_result_status, exam_result_published_at,
		       exam_result_is_deleted, exam_result_deleted_at,
		       exam_result_created_at, exam_result_updated_at
		FROM exam_results
		WHERE exam_result_exam_registration_id=$1 AND exam_result_is_deleted=FALSE`,
		registrationID,
	).Scan(
		&e.ExamResultID, &e.ExamResultExamRegistrationID,
		&e.ExamResultListeningScore, &e.ExamResultReadingScore,
		&e.ExamResultWritingScore, &e.ExamResultTotalScore,
		&e.ExamResultStatus, &e.ExamResultPublishedAt,
		&e.ExamResultIsDeleted, &e.ExamResultDeletedAt,
		&e.ExamResultCreatedAt, &e.ExamResultUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamResultResponseDTO{}, errors.New("result not found")
	}
	if err != nil {
		return response.ExamResultResponseDTO{}, fmt.Errorf("result: get: %w", err)
	}
	return ToExamResultResponseDTO(e), nil
}

// CreateRecheckRequest submits a recheck and returns its public DTO.
func (s *Service) CreateRecheckRequest(ctx context.Context, req request.CreateRecheckRequestDTO) (response.RecheckResponseDTO, error) {
	var e ExamRecheckRequestEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO exam_recheck_requests
		       (exam_recheck_exam_result_id, exam_recheck_reason, exam_recheck_status)
		VALUES ($1,$2,'submitted')
		RETURNING exam_recheck_id, exam_recheck_exam_result_id, exam_recheck_reason,
		          exam_recheck_status, exam_recheck_reviewed_by_account_id,
		          exam_recheck_reviewed_at, exam_recheck_new_total_score,
		          exam_recheck_is_deleted, exam_recheck_deleted_at,
		          exam_recheck_created_at, exam_recheck_updated_at`,
		req.ExamResultID, req.Reason,
	).Scan(
		&e.ExamRecheckID, &e.ExamRecheckExamResultID, &e.ExamRecheckReason,
		&e.ExamRecheckStatus, &e.ExamRecheckReviewedByAccountID,
		&e.ExamRecheckReviewedAt, &e.ExamRecheckNewTotalScore,
		&e.ExamRecheckIsDeleted, &e.ExamRecheckDeletedAt,
		&e.ExamRecheckCreatedAt, &e.ExamRecheckUpdatedAt,
	)
	if err != nil {
		return response.RecheckResponseDTO{}, fmt.Errorf("result: create recheck: %w", err)
	}
	return ToRecheckResponseDTO(e), nil
}

// ReviewRecheckRequest records a reviewer's decision and returns the updated DTO.
func (s *Service) ReviewRecheckRequest(ctx context.Context, in ReviewRecheckInput) (response.RecheckResponseDTO, error) {
	if !in.Status.Valid() {
		return response.RecheckResponseDTO{}, fmt.Errorf("invalid recheck status: %s", in.Status)
	}
	var e ExamRecheckRequestEntity
	err := s.db.QueryRowContext(ctx, `
		UPDATE exam_recheck_requests
		SET exam_recheck_status                  = $1,
		    exam_recheck_reviewed_by_account_id   = $2,
		    exam_recheck_reviewed_at              = NOW(),
		    exam_recheck_new_total_score          = $3,
		    exam_recheck_updated_at               = NOW()
		WHERE exam_recheck_id=$4 AND exam_recheck_is_deleted=FALSE
		RETURNING exam_recheck_id, exam_recheck_exam_result_id, exam_recheck_reason,
		          exam_recheck_status, exam_recheck_reviewed_by_account_id,
		          exam_recheck_reviewed_at, exam_recheck_new_total_score,
		          exam_recheck_is_deleted, exam_recheck_deleted_at,
		          exam_recheck_created_at, exam_recheck_updated_at`,
		in.Status, in.ReviewerID, in.NewTotalScore, in.RecheckID,
	).Scan(
		&e.ExamRecheckID, &e.ExamRecheckExamResultID, &e.ExamRecheckReason,
		&e.ExamRecheckStatus, &e.ExamRecheckReviewedByAccountID,
		&e.ExamRecheckReviewedAt, &e.ExamRecheckNewTotalScore,
		&e.ExamRecheckIsDeleted, &e.ExamRecheckDeletedAt,
		&e.ExamRecheckCreatedAt, &e.ExamRecheckUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.RecheckResponseDTO{}, errors.New("recheck request not found")
	}
	if err != nil {
		return response.RecheckResponseDTO{}, fmt.Errorf("result: review: %w", err)
	}
	return ToRecheckResponseDTO(e), nil
}
