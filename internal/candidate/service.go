package candidate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/enum"
)

// Service handles candidate profiles and documents.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
}

// NewService creates a candidate Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

// CreateProfile inserts a new candidate and returns its public DTO.
func (s *Service) CreateProfile(ctx context.Context, req request.CreateProfileRequestDTO, accountID int64) (response.CandidateResponseDTO, error) {
	dob, err := time.Parse("2006-01-02", req.DOB)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: invalid dob: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e CandidateEntity
	err = tx.QueryRowContext(ctx, `
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
		accountID, req.FullName, req.FullNameCN,
		dob, req.Gender, req.Ethnicity, req.Religion,
		req.Birthplace, req.Nationality, req.MotherTongue, req.ChineseStudyYears,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateBirthplace, &e.CandidateNationality, &e.CandidateMotherTongue,
		&e.CandidateChineseStudyYears, &e.CandidateIsDeleted, &e.CandidateDeletedAt,
		&e.CandidateCreatedAt, &e.CandidateUpdatedAt,
	)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: create profile: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"full_name": e.CandidateFullName, "gender": string(e.CandidateGender)})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityCandidate.String(),
		EntityID:       e.CandidateID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: commit: %w", err)
	}
	return ToCandidateResponseDTO(e), nil
}

// UpdateProfile applies partial updates and returns the updated public DTO.
func (s *Service) UpdateProfile(ctx context.Context, candidateID int64, req request.UpdateProfileRequestDTO) (response.CandidateResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e CandidateEntity
	err = tx.QueryRowContext(ctx, `
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
		req.FullName, req.FullNameCN, req.Ethnicity, req.Religion,
		req.Birthplace, req.Nationality, req.MotherTongue, req.ChineseStudyYears, candidateID,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateBirthplace, &e.CandidateNationality, &e.CandidateMotherTongue,
		&e.CandidateChineseStudyYears, &e.CandidateIsDeleted, &e.CandidateDeletedAt,
		&e.CandidateCreatedAt, &e.CandidateUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CandidateResponseDTO{}, errors.New("candidate not found")
	}
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: update profile: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"full_name": e.CandidateFullName, "nationality": e.CandidateNationality})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &e.CandidateAccountID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityCandidate.String(),
		EntityID:       candidateID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: commit: %w", err)
	}
	return ToCandidateResponseDTO(e), nil
}

// UploadDocument inserts a new document and returns its public DTO.
func (s *Service) UploadDocument(ctx context.Context, candidateID int64, req request.UploadDocumentRequestDTO) (response.DocumentResponseDTO, error) {
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: invalid issue_date: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e CandidateDocumentEntity
	err = tx.QueryRowContext(ctx, `
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
		candidateID, enum.DocumentType(req.DocType), req.DocNumber,
		issueDate, req.IssuePlace,
		req.FrontImageURL, req.BackImageURL, req.PortraitImageURL,
		req.WardID, req.AddressDetail,
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
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: upload document: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"doc_type": string(e.CandidateDocumentType), "doc_number": e.CandidateDocumentNumber})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
		EntityID:       e.CandidateDocumentID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: commit: %w", err)
	}
	return ToDocumentResponseDTO(e), nil
}
