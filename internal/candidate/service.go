package candidate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Service handles candidate profiles and documents.
type Service struct{ db *sql.DB }

// NewService creates a candidate Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateProfile inserts a new candidate and returns its public DTO.
func (s *Service) CreateProfile(ctx context.Context, in CreateProfileInput) (CandidateResponseDTO, error) {
	var e CandidateEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidates
		       (candidate_account_id, candidate_full_name, candidate_full_name_cn,
		        candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		        candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		        candidate_chinese_study_years)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),
		        NULLIF($8,''),COALESCE(NULLIF($9,''),'Việt Nam'),NULLIF($10,''),$11)
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		          candidate_chinese_study_years, candidate_is_deleted, candidate_deleted_at,
		          candidate_created_at, candidate_updated_at`,
		in.AccountID, in.FullName, in.FullNameCN,
		in.DOB, in.Gender, in.Ethnicity, in.Religion,
		in.Birthplace, in.Nationality, in.MotherTongue, in.ChineseStudyYears,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateBirthplace, &e.CandidateNationality, &e.CandidateMotherTongue,
		&e.CandidateChineseStudyYears, &e.CandidateIsDeleted, &e.CandidateDeletedAt,
		&e.CandidateCreatedAt, &e.CandidateUpdatedAt,
	)
	if err != nil {
		return CandidateResponseDTO{}, fmt.Errorf("candidate: create profile: %w", err)
	}
	return ToCandidateResponseDTO(e), nil
}

// UpdateProfile applies partial updates and returns the updated public DTO.
func (s *Service) UpdateProfile(ctx context.Context, candidateID int64, in UpdateProfileInput) (CandidateResponseDTO, error) {
	var e CandidateEntity
	err := s.db.QueryRowContext(ctx, `
		UPDATE candidates
		SET candidate_full_name           = COALESCE(NULLIF($1,''), candidate_full_name),
		    candidate_full_name_cn        = COALESCE(NULLIF($2,''), candidate_full_name_cn),
		    candidate_ethnicity           = COALESCE(NULLIF($3,''), candidate_ethnicity),
		    candidate_religion            = COALESCE(NULLIF($4,''), candidate_religion),
		    candidate_birthplace          = COALESCE(NULLIF($5,''), candidate_birthplace),
		    candidate_nationality         = COALESCE(NULLIF($6,''), candidate_nationality),
		    candidate_mother_tongue       = COALESCE(NULLIF($7,''), candidate_mother_tongue),
		    candidate_chinese_study_years = COALESCE($8, candidate_chinese_study_years),
		    candidate_updated_at          = NOW()
		WHERE candidate_id = $9 AND candidate_is_deleted = FALSE
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		          candidate_chinese_study_years, candidate_is_deleted, candidate_deleted_at,
		          candidate_created_at, candidate_updated_at`,
		in.FullName, in.FullNameCN, in.Ethnicity, in.Religion,
		in.Birthplace, in.Nationality, in.MotherTongue, in.ChineseStudyYears, candidateID,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateBirthplace, &e.CandidateNationality, &e.CandidateMotherTongue,
		&e.CandidateChineseStudyYears, &e.CandidateIsDeleted, &e.CandidateDeletedAt,
		&e.CandidateCreatedAt, &e.CandidateUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateResponseDTO{}, errors.New("candidate not found")
	}
	if err != nil {
		return CandidateResponseDTO{}, fmt.Errorf("candidate: update profile: %w", err)
	}
	return ToCandidateResponseDTO(e), nil
}

// UploadDocument inserts a new document and returns its public DTO.
func (s *Service) UploadDocument(ctx context.Context, in UploadDocumentInput) (DocumentResponseDTO, error) {
	var e CandidateDocumentEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidate_documents
		       (candidate_document_candidate_id, candidate_document_type, candidate_document_number,
		        candidate_document_issue_date, candidate_document_issue_place,
		        candidate_document_front_image_url, candidate_document_back_image_url,
		        candidate_document_portrait_image_url, candidate_document_ward_id,
		        candidate_document_address_detail)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,NULLIF($10,''))
		RETURNING candidate_document_id, candidate_document_candidate_id,
		          candidate_document_type, candidate_document_number,
		          candidate_document_issue_date, candidate_document_issue_place,
		          candidate_document_front_image_url, candidate_document_back_image_url,
		          candidate_document_portrait_image_url,
		          candidate_document_verification_status,
		          candidate_document_ward_id, candidate_document_address_detail,
		          candidate_document_is_deleted, candidate_document_deleted_at,
		          candidate_document_created_at, candidate_document_updated_at`,
		in.CandidateID, in.DocType, in.DocNumber,
		in.IssueDate, in.IssuePlace,
		in.FrontImageURL, in.BackImageURL, in.PortraitImageURL,
		in.WardID, in.AddressDetail,
	).Scan(
		&e.CandidateDocumentID, &e.CandidateDocumentCandidateID,
		&e.CandidateDocumentType, &e.CandidateDocumentNumber,
		&e.CandidateDocumentIssueDate, &e.CandidateDocumentIssuePlace,
		&e.CandidateDocumentFrontImageURL, &e.CandidateDocumentBackImageURL,
		&e.CandidateDocumentPortraitImageURL,
		&e.CandidateDocumentVerificationStatus,
		&e.CandidateDocumentWardID, &e.CandidateDocumentAddressDetail,
		&e.CandidateDocumentIsDeleted, &e.CandidateDocumentDeletedAt,
		&e.CandidateDocumentCreatedAt, &e.CandidateDocumentUpdatedAt,
	)
	if err != nil {
		return DocumentResponseDTO{}, fmt.Errorf("candidate: upload document: %w", err)
	}
	return ToDocumentResponseDTO(e), nil
}

// nullStr converts empty string to nil *string for optional nullable columns.
func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	st := s
	return &st
}

var _ = time.Now // keep import used
