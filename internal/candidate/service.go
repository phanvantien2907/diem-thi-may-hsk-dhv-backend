package candidate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Service handles candidate profile and document operations.
type Service struct{ db *sql.DB }

// NewService creates a candidate Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// CreateProfile inserts a candidate profile linked to an account.
func (s *Service) CreateProfile(ctx context.Context, in CreateProfileInput) (*Candidate, error) {
	var c Candidate
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidates
		       (candidate_account_id, candidate_full_name, candidate_full_name_cn,
		        candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		        candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		        candidate_chinese_study_years)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		          candidate_chinese_study_years, candidate_is_deleted,
		          candidate_created_at, candidate_updated_at`,
		in.AccountID, in.FullName, nullStr(in.FullNameCN),
		in.DOB, in.Gender, nullStr(in.Ethnicity), nullStr(in.Religion),
		nullStr(in.Birthplace), in.Nationality, nullStr(in.MotherTongue),
		in.ChineseStudyYears,
	).Scan(
		&c.CandidateID, &c.CandidateAccountID, &c.CandidateFullName, &c.CandidateFullNameCN,
		&c.CandidateDOB, &c.CandidateGender, &c.CandidateEthnicity, &c.CandidateReligion,
		&c.CandidateBirthplace, &c.CandidateNationality, &c.CandidateMotherTongue,
		&c.CandidateChineseStudyYears, &c.CandidateIsDeleted,
		&c.CandidateCreatedAt, &c.CandidateUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("candidate: create profile: %w", err)
	}
	return &c, nil
}

// UpdateProfile updates mutable candidate fields.
func (s *Service) UpdateProfile(ctx context.Context, candidateID int64, in UpdateProfileInput) (*Candidate, error) {
	var c Candidate
	err := s.db.QueryRowContext(ctx, `
		UPDATE candidates
		SET candidate_full_name           = $1,
		    candidate_full_name_cn        = $2,
		    candidate_ethnicity           = $3,
		    candidate_religion            = $4,
		    candidate_birthplace          = $5,
		    candidate_nationality         = $6,
		    candidate_mother_tongue       = $7,
		    candidate_chinese_study_years = $8,
		    candidate_updated_at          = NOW()
		WHERE candidate_id = $9 AND candidate_is_deleted = FALSE
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_birthplace, candidate_nationality, candidate_mother_tongue,
		          candidate_chinese_study_years, candidate_is_deleted,
		          candidate_created_at, candidate_updated_at`,
		in.FullName, nullStr(in.FullNameCN), nullStr(in.Ethnicity),
		nullStr(in.Religion), nullStr(in.Birthplace), in.Nationality,
		nullStr(in.MotherTongue), in.ChineseStudyYears, candidateID,
	).Scan(
		&c.CandidateID, &c.CandidateAccountID, &c.CandidateFullName, &c.CandidateFullNameCN,
		&c.CandidateDOB, &c.CandidateGender, &c.CandidateEthnicity, &c.CandidateReligion,
		&c.CandidateBirthplace, &c.CandidateNationality, &c.CandidateMotherTongue,
		&c.CandidateChineseStudyYears, &c.CandidateIsDeleted,
		&c.CandidateCreatedAt, &c.CandidateUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("candidate not found")
	}
	if err != nil {
		return nil, fmt.Errorf("candidate: update profile: %w", err)
	}
	return &c, nil
}

// UploadDocument inserts an identity document record.
func (s *Service) UploadDocument(ctx context.Context, in UploadDocumentInput) (*CandidateDocument, error) {
	var d CandidateDocument
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO candidate_documents
		       (candidate_document_candidate_id, candidate_document_type,
		        candidate_document_number, candidate_document_issue_date,
		        candidate_document_issue_place, candidate_document_front_image_url,
		        candidate_document_back_image_url, candidate_document_portrait_image_url,
		        candidate_document_ward_id, candidate_document_address_detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING candidate_document_id, candidate_document_candidate_id,
		          candidate_document_type, candidate_document_number,
		          candidate_document_issue_date, candidate_document_issue_place,
		          candidate_document_front_image_url, candidate_document_back_image_url,
		          candidate_document_portrait_image_url,
		          candidate_document_verification_status,
		          candidate_document_ward_id, candidate_document_address_detail,
		          candidate_document_is_deleted,
		          candidate_document_created_at, candidate_document_updated_at`,
		in.CandidateID, in.DocType, in.DocNumber, in.IssueDate, in.IssuePlace,
		in.FrontImageURL, nullStr(in.BackImageURL), in.PortraitImageURL,
		in.WardID, nullStr(in.AddressDetail),
	).Scan(
		&d.CandidateDocumentID, &d.CandidateDocumentCandidateID,
		&d.CandidateDocumentType, &d.CandidateDocumentNumber,
		&d.CandidateDocumentIssueDate, &d.CandidateDocumentIssuePlace,
		&d.CandidateDocumentFrontImageURL, &d.CandidateDocumentBackImageURL,
		&d.CandidateDocumentPortraitImageURL,
		&d.CandidateDocumentVerificationStatus,
		&d.CandidateDocumentWardID, &d.CandidateDocumentAddressDetail,
		&d.CandidateDocumentIsDeleted,
		&d.CandidateDocumentCreatedAt, &d.CandidateDocumentUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("candidate: upload document: %w", err)
	}
	return &d, nil
}

// VerifyDocument updates a document's verification status (admin/staff).
func (s *Service) VerifyDocument(ctx context.Context, docID int64, status DocumentVerificationStatus) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = $1, candidate_document_updated_at = NOW()
		WHERE candidate_document_id = $2 AND candidate_document_is_deleted = FALSE`,
		status, docID,
	)
	if err != nil {
		return fmt.Errorf("candidate: verify document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("document not found")
	}
	return nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
