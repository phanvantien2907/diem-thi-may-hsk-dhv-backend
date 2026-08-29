package candidate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/candidate/enum"
	"github.com/hskvinh/backend/internal/media"
)

// Service handles candidate profiles and documents.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	mediaSvc *media.Service
}

// NewService creates a candidate Service.
func NewService(db *sql.DB, auditSvc audit.Service, mediaSvc *media.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc, mediaSvc: mediaSvc}
}

// CreateProfile inserts a new candidate and returns its public DTO.
func (s *Service) CreateProfile(ctx context.Context, req request.CreateProfileRequestDTO, accountID int64) (response.CandidateResponseDTO, error) {

	// validate date of birth
	dob, err := time.Parse("2006-01-02", req.DOB)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: invalid dob: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CandidateEntity
	err = tx.QueryRowContext(ctx, `
		INSERT INTO candidates
		       (candidate_account_id, candidate_full_name, candidate_full_name_cn,
		        candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		        candidate_nationality,
		        candidate_chinese_study_years)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),
		        COALESCE(NULLIF($8,''),'Việt Nam'),$9)
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_nationality, candidate_chinese_study_years`,
		accountID, req.FullName, req.FullNameCN,
		dob, req.Gender, req.Ethnicity, req.Religion,
		req.Nationality, req.ChineseStudyYears,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateNationality, &e.CandidateChineseStudyYears,
	)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: create profile: %w", err)
	}

	// write log
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

	// validate date of birth
	dob, err := time.Parse("2006-01-02", req.DOB)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: invalid dob: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CandidateEntity
	err = tx.QueryRowContext(ctx, `
		UPDATE candidates
		SET candidate_full_name           = COALESCE(NULLIF($1,''), candidate_full_name),
		    candidate_full_name_cn        = COALESCE(NULLIF($2,''), candidate_full_name_cn),
			candidate_dob                 = $3,
		    candidate_ethnicity           = COALESCE(NULLIF($4,''), candidate_ethnicity),
		    candidate_religion            = COALESCE(NULLIF($5,''), candidate_religion),
		    candidate_nationality         = COALESCE(NULLIF($6,''), candidate_nationality),
		    candidate_chinese_study_years = COALESCE($7, candidate_chinese_study_years)
		WHERE candidate_id = $8 AND candidate_is_deleted = FALSE
		RETURNING candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
		          candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		          candidate_nationality, candidate_chinese_study_years`,
		req.FullName, req.FullNameCN, dob, req.Ethnicity, req.Religion, req.Nationality, req.ChineseStudyYears, candidateID,
	).Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender, &e.CandidateEthnicity, &e.CandidateReligion,
		&e.CandidateNationality, &e.CandidateChineseStudyYears,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CandidateResponseDTO{}, errors.New("candidate not found")
	}
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: update profile: %w", err)
	}

	// write log
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

	// validate issue date
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: invalid issue_date: %w", err)
	}

	var (
		frontURL    string
		backURL     string
		portraitURL string

		// Flag to indicate if everything succeeded. If false during defer, we clean up.
		isSuccess bool

		wg      sync.WaitGroup
		errChan = make(chan error, 3) // buffer of 3 for 3 possible concurrent tasks
	)

	// Clean up any successfully uploaded images if we fail at any point later
	defer func() {
		if !isSuccess {
			// using context.Background() because original ctx might be cancelled/expired
			if frontURL != "" {
				_ = s.mediaSvc.DeleteImage(context.Background(), frontURL)
			}
			if backURL != "" {
				_ = s.mediaSvc.DeleteImage(context.Background(), backURL)
			}
			if portraitURL != "" {
				_ = s.mediaSvc.DeleteImage(context.Background(), portraitURL)
			}
		}
	}()

	uploadTask := func(fileHeader *multipart.FileHeader, destURL *string, name string) {
		defer wg.Done()
		file, err := fileHeader.Open()
		if err != nil {
			errChan <- fmt.Errorf("candidate: open %s image failed: %w", name, err)
			return
		}
		defer file.Close()
		url, err := s.mediaSvc.UploadImage(ctx, file)
		if err != nil {
			errChan <- fmt.Errorf("candidate: upload %s image failed: %w", name, err)
			return
		}
		*destURL = url
	}

	wg.Add(2)
	go uploadTask(req.FrontImage, &frontURL, "front")
	go uploadTask(req.PortraitImage, &portraitURL, "portrait")

	if req.BackImage != nil {
		wg.Add(1)
		go uploadTask(req.BackImage, &backURL, "back")
	}

	// Wait for all uploads to complete
	wg.Wait()
	close(errChan)

	// Check if any upload failed
	for uploadErr := range errChan {
		if uploadErr != nil {
			return response.DocumentResponseDTO{}, uploadErr // defer cleans up the ones that succeeded
		}
	}

	// All uploads succeeded, now insert to DB
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CandidateDocumentEntity
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
		frontURL, backURL, portraitURL,
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
	
	isSuccess = true // Mark success so defer cleanup doesn't delete Cloudinary images
	return ToDocumentResponseDTO(e), nil
}
