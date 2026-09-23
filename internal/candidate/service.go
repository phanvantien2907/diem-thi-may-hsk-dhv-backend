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
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/candidate/enum"
	"github.com/hskvinh/backend/internal/utils"
)

// MediaService defines the required media upload functionality for candidates.
type MediaService interface {
	GenerateSignature() (string, int64, string, string, error)
	DeleteImage(ctx context.Context, publicID string) error
}

// Service handles candidate profiles and documents.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	mediaSvc MediaService
}

// ErrActiveRegistration is returned when a candidate has an active registration
// and attempts to modify their profile.
var ErrActiveRegistration = errors.New("không thể sửa hồ sơ khi đã có đăng ký ca thi đang hoạt động")

// NewService creates a candidate Service.
func NewService(db *sql.DB, auditSvc audit.Service, mediaSvc MediaService) *Service {
	return &Service{db: db, auditSvc: auditSvc, mediaSvc: mediaSvc}
}

// scanCandidate scans the standard candidate projection (no soft-delete cols).
func scanCandidate(row *sql.Row, e *entities.CandidateEntity) error {
	return row.Scan(
		&e.CandidateID, &e.CandidateAccountID, &e.CandidateFullName, &e.CandidateFullNameCN,
		&e.CandidateDOB, &e.CandidateGender,
		&e.CandidateEthnicity, &e.CandidateReligion, &e.CandidateBirthplace,
		&e.CandidateNationality, &e.CandidateMotherTongue,
		&e.CandidateChineseStudyYears,
	)
}

// candidateSelectCols is the SELECT list that matches scanCandidate.
const candidateSelectCols = `
	candidate_id, candidate_account_id, candidate_full_name, candidate_full_name_cn,
	candidate_dob, candidate_gender,
	candidate_ethnicity, candidate_religion, candidate_birthplace,
	candidate_nationality, candidate_mother_tongue,
	candidate_chinese_study_years`

// docSelectCols is the SELECT list for a document (no is_deleted/deleted_at).
const docSelectCols = `
	candidate_document_id, candidate_document_candidate_id,
	candidate_document_type, candidate_document_number,
	candidate_document_issue_date, candidate_document_issue_place,
	candidate_document_front_image_url, candidate_document_back_image_url,
	candidate_document_portrait_image_url,
	candidate_document_verification_status, candidate_document_rejection_reason,
	candidate_document_ward_id, candidate_document_address_detail`

func scanDocument(row *sql.Row, e *entities.CandidateDocumentEntity) error {
	return row.Scan(
		&e.CandidateDocumentID, &e.CandidateDocumentCandidateID,
		&e.CandidateDocumentType, &e.CandidateDocumentNumber,
		&e.CandidateDocumentIssueDate, &e.CandidateDocumentIssuePlace,
		&e.CandidateDocumentFrontImageURL, &e.CandidateDocumentBackImageURL,
		&e.CandidateDocumentPortraitImageURL,
		&e.CandidateDocumentVerificationStatus, &e.CandidateDocumentRejectionReason,
		&e.CandidateDocumentWardID, &e.CandidateDocumentAddressDetail,
	)
}

// CreateProfile inserts a new candidate profile for the authenticated account.
func (s *Service) CreateProfile(ctx context.Context, req request.CreateProfileRequestDTO, accountID int64) (response.CandidateResponseDTO, error) {
	dob, err := time.Parse("2006-01-02", req.DOB)
	if err != nil {
		return response.CandidateResponseDTO{}, errors.New("candidate: ngày sinh không hợp lệ (định dạng YYYY-MM-DD)")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CandidateEntity
	err = scanCandidate(tx.QueryRowContext(ctx, `
		INSERT INTO candidates
		       (candidate_account_id, candidate_full_name, candidate_full_name_cn,
		        candidate_dob, candidate_gender, candidate_ethnicity, candidate_religion,
		        candidate_nationality, candidate_chinese_study_years)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),
		        COALESCE(NULLIF($8,''),'Việt Nam'),$9)
		RETURNING `+candidateSelectCols,
		accountID, req.FullName, req.FullNameCN,
		dob, req.Gender, req.Ethnicity, req.Religion,
		req.Nationality, req.ChineseStudyYears,
	), &e)
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

// GetMyProfile returns the candidate profile for the authenticated account.
func (s *Service) GetMyProfile(ctx context.Context, accountID int64) (response.CandidateResponseDTO, error) {
	var e entities.CandidateEntity
	err := scanCandidate(s.db.QueryRowContext(ctx, `
		SELECT `+candidateSelectCols+`
		FROM candidates
		WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CandidateResponseDTO{}, errors.New("chưa có hồ sơ thí sinh")
	}
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: get profile: %w", err)
	}
	return ToCandidateResponseDTO(e), nil
}

// GetMyDocuments returns all non-deleted identity documents for the authenticated account.
func (s *Service) GetMyDocuments(ctx context.Context, accountID int64) ([]entities.CandidateDocumentEntity, error) {
	// Resolve candidate_id from accountID (ownership check)
	var candidateID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT candidate_id FROM candidates WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`,
		accountID,
	).Scan(&candidateID); errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("chưa có hồ sơ thí sinh")
	} else if err != nil {
		return nil, fmt.Errorf("candidate: lookup: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+docSelectCols+`
		FROM candidate_documents
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted = FALSE
		ORDER BY candidate_document_created_at DESC`,
		candidateID,
	)
	if err != nil {
		return nil, fmt.Errorf("candidate: get documents: %w", err)
	}
	defer rows.Close()

	var docs []entities.CandidateDocumentEntity
	for rows.Next() {
		var d entities.CandidateDocumentEntity
		if err := rows.Scan(
			&d.CandidateDocumentID, &d.CandidateDocumentCandidateID,
			&d.CandidateDocumentType, &d.CandidateDocumentNumber,
			&d.CandidateDocumentIssueDate, &d.CandidateDocumentIssuePlace,
			&d.CandidateDocumentFrontImageURL, &d.CandidateDocumentBackImageURL,
			&d.CandidateDocumentPortraitImageURL,
			&d.CandidateDocumentVerificationStatus, &d.CandidateDocumentRejectionReason,
			&d.CandidateDocumentWardID, &d.CandidateDocumentAddressDetail,
		); err != nil {
			return nil, fmt.Errorf("candidate: scan document: %w", err)
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate: documents rows: %w", err)
	}
	if docs == nil {
		docs = []entities.CandidateDocumentEntity{}
	}
	return docs, nil
}

// PatchMyProfile applies partial updates to the authenticated candidate's profile.
// Business rules:
//  1. DOB and Gender are immutable (omitted from update).
//  2. Blocked if any active registration (pending_payment or confirmed) exists.
//  3. All document verification statuses are reset to 'pending' on update.
func (s *Service) PatchMyProfile(ctx context.Context, accountID int64, req request.PatchProfileRequestDTO) (response.CandidateResponseDTO, error) {
	// 1. Fetch candidate id to verify ownership
	var candidateID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT candidate_id FROM candidates WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`,
		accountID,
	).Scan(&candidateID); errors.Is(err, sql.ErrNoRows) {
		return response.CandidateResponseDTO{}, errors.New("chưa có hồ sơ thí sinh")
	} else if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: lookup: %w", err)
	}

	// 2. Block if active registration exists
	var hasActive bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM exam_registrations
			WHERE exam_registration_candidate_id = $1
			  AND exam_registration_status IN ('pending_payment','confirmed')
			  AND exam_registration_is_deleted = FALSE
		)`, candidateID,
	).Scan(&hasActive); err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: check registration: %w", err)
	}
	if hasActive {
		return response.CandidateResponseDTO{}, ErrActiveRegistration
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 3. Update profile (DOB/Gender unchanged)
	var e entities.CandidateEntity
	err = scanCandidate(tx.QueryRowContext(ctx, `
		UPDATE candidates
		SET candidate_full_name           = COALESCE(NULLIF($1,''), candidate_full_name),
		    candidate_full_name_cn        = COALESCE(NULLIF($2,''), candidate_full_name_cn),
		    candidate_ethnicity           = COALESCE(NULLIF($3,''), candidate_ethnicity),
		    candidate_religion            = COALESCE(NULLIF($4,''), candidate_religion),
		    candidate_nationality         = COALESCE(NULLIF($5,''), candidate_nationality),
		    candidate_chinese_study_years = COALESCE($6, candidate_chinese_study_years)
		WHERE candidate_id = $7 AND candidate_is_deleted = FALSE
		RETURNING `+candidateSelectCols,
		req.FullName, req.FullNameCN, req.Ethnicity, req.Religion, req.Nationality,
		req.ChineseStudyYears, candidateID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.CandidateResponseDTO{}, errors.New("chưa có hồ sơ thí sinh")
	}
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: patch profile: %w", err)
	}

	// 4. Reset document verification status → pending (anti-fraud)
	_, _ = tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'pending',
		    candidate_document_rejection_reason    = NULL
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status != 'pending'`,
		candidateID,
	)

	newVal, _ := json.Marshal(map[string]any{"full_name": e.CandidateFullName, "nationality": e.CandidateNationality})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
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

// GetUploadSignature generates a secure signature for direct client-side uploading.
func (s *Service) GetUploadSignature(ctx context.Context) (response.UploadSignatureResponseDTO, error) {
	sig, ts, key, cloud, err := s.mediaSvc.GenerateSignature()
	if err != nil {
		return response.UploadSignatureResponseDTO{}, fmt.Errorf("candidate: get signature: %w", err)
	}
	return response.UploadSignatureResponseDTO{
		Signature: sig,
		Timestamp: ts,
		APIKey:    key,
		CloudName: cloud,
		Folder:    "hsk_vinh_university/candidates",
	}, nil
}

// UploadDocument saves the direct-uploaded identity images to a document record.
func (s *Service) UploadDocument(ctx context.Context, accountID int64, req request.UploadDocumentRequestDTO) (response.DocumentResponseDTO, error) {
	// Resolve candidate_id from accountID (authoritative ownership check)
	var candidateID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT candidate_id FROM candidates WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`,
		accountID,
	).Scan(&candidateID); errors.Is(err, sql.ErrNoRows) {
		return response.DocumentResponseDTO{}, errors.New("chưa có hồ sơ thí sinh")
	} else if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: lookup: %w", err)
	}

	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.DocumentResponseDTO{}, errors.New("candidate: ngày cấp không hợp lệ (định dạng YYYY-MM-DD)")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.CandidateDocumentEntity
	err = scanDocument(tx.QueryRowContext(ctx, `
		INSERT INTO candidate_documents
		       (candidate_document_candidate_id, candidate_document_type, candidate_document_number,
		        candidate_document_issue_date, candidate_document_issue_place,
		        candidate_document_front_image_url, candidate_document_back_image_url,
		        candidate_document_portrait_image_url, candidate_document_ward_id,
		        candidate_document_address_detail)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,NULLIF($10,''))
		RETURNING `+docSelectCols,
		candidateID, enum.DocumentType(req.DocType), req.DocNumber,
		issueDate, req.IssuePlace,
		req.FrontImageURL, req.BackImageURL, req.PortraitImageURL,
		req.WardID, req.AddressDetail,
	), &e)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: upload document: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"doc_type": string(e.CandidateDocumentType), "doc_number": e.CandidateDocumentNumber})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
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

// AdminListCandidates returns a paginated list of candidates filtered by document verification status.
func (s *Service) AdminListCandidates(ctx context.Context, req request.AdminListCandidatesRequestDTO) ([]response.AdminCandidateResponseDTO, utils.Meta, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 20
	}

	// Build WHERE based on status filter (join to latest document)
	statusFilter := ""
	var args []any
	if req.Status != "" {
		statusFilter = "AND d.candidate_document_verification_status = $3"
		args = append(args, req.Status)
	}

	offset := (req.Page - 1) * req.Limit

	// Count total
	countArgs := append([]any{req.Page, req.Limit}, args...)
	_ = countArgs
	var countQuery string
	var countArgs2 []any
	if req.Status != "" {
		countQuery = `
			SELECT COUNT(DISTINCT c.candidate_id)
			FROM candidates c
			JOIN candidate_documents d ON d.candidate_document_candidate_id = c.candidate_id
			  AND d.candidate_document_is_deleted = FALSE
			WHERE c.candidate_is_deleted = FALSE
			  AND d.candidate_document_verification_status = $1`
		countArgs2 = []any{req.Status}
	} else {
		countQuery = `SELECT COUNT(*) FROM candidates WHERE candidate_is_deleted = FALSE`
		countArgs2 = nil
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs2...).Scan(&total); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin list count: %w", err)
	}

	if total == 0 {
		return []response.AdminCandidateResponseDTO{}, utils.Meta{
			Page: req.Page, PerPage: req.Limit, TotalItems: 0, TotalPages: 0,
		}, nil
	}

	// Fetch page — use DISTINCT ON to get one row per candidate with latest document
	var rows *sql.Rows
	var err error
	if req.Status != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT DISTINCT ON (c.candidate_id)
				c.candidate_id, c.candidate_account_id, c.candidate_full_name, c.candidate_full_name_cn,
				c.candidate_dob, c.candidate_gender,
				c.candidate_ethnicity, c.candidate_religion, c.candidate_birthplace,
				c.candidate_nationality, c.candidate_mother_tongue,
				c.candidate_chinese_study_years,
				d.candidate_document_id, d.candidate_document_candidate_id,
				d.candidate_document_type, d.candidate_document_number,
				d.candidate_document_issue_date, d.candidate_document_issue_place,
				d.candidate_document_front_image_url, d.candidate_document_back_image_url,
				d.candidate_document_portrait_image_url,
				d.candidate_document_verification_status, d.candidate_document_rejection_reason,
				d.candidate_document_ward_id, d.candidate_document_address_detail
			FROM candidates c
			JOIN candidate_documents d ON d.candidate_document_candidate_id = c.candidate_id
			  AND d.candidate_document_is_deleted = FALSE
			WHERE c.candidate_is_deleted = FALSE
			  `+statusFilter+`
			ORDER BY c.candidate_id, d.candidate_document_created_at DESC
			LIMIT $1 OFFSET $2`,
			req.Limit, offset, req.Status,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT DISTINCT ON (c.candidate_id)
				c.candidate_id, c.candidate_account_id, c.candidate_full_name, c.candidate_full_name_cn,
				c.candidate_dob, c.candidate_gender,
				c.candidate_ethnicity, c.candidate_religion, c.candidate_birthplace,
				c.candidate_nationality, c.candidate_mother_tongue,
				c.candidate_chinese_study_years,
				d.candidate_document_id, d.candidate_document_candidate_id,
				d.candidate_document_type, d.candidate_document_number,
				d.candidate_document_issue_date, d.candidate_document_issue_place,
				d.candidate_document_front_image_url, d.candidate_document_back_image_url,
				d.candidate_document_portrait_image_url,
				d.candidate_document_verification_status, d.candidate_document_rejection_reason,
				d.candidate_document_ward_id, d.candidate_document_address_detail
			FROM candidates c
			LEFT JOIN candidate_documents d ON d.candidate_document_candidate_id = c.candidate_id
			  AND d.candidate_document_is_deleted = FALSE
			WHERE c.candidate_is_deleted = FALSE
			ORDER BY c.candidate_id, d.candidate_document_created_at DESC
			LIMIT $1 OFFSET $2`,
			req.Limit, offset,
		)
	}
	if err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin list query: %w", err)
	}
	defer rows.Close()

	items := make([]response.AdminCandidateResponseDTO, 0, req.Limit)
	for rows.Next() {
		var c entities.CandidateEntity
		var d entities.CandidateDocumentEntity
		// document fields can be NULL when no document exists (LEFT JOIN case)
		var (
			docID, docCandID            sql.NullInt64
			docType, docNumber          sql.NullString
			docIssueDate                sql.NullTime
			docIssuePlace               sql.NullString
			docFrontURL, docPortraitURL sql.NullString
			docBackURL                  sql.NullString
			docStatus, docRejection     sql.NullString
			docWardID                   sql.NullInt64
			docAddressDetail            sql.NullString
		)
		if err := rows.Scan(
			&c.CandidateID, &c.CandidateAccountID, &c.CandidateFullName, &c.CandidateFullNameCN,
			&c.CandidateDOB, &c.CandidateGender,
			&c.CandidateEthnicity, &c.CandidateReligion, &c.CandidateBirthplace,
			&c.CandidateNationality, &c.CandidateMotherTongue,
			&c.CandidateChineseStudyYears,
			&docID, &docCandID,
			&docType, &docNumber,
			&docIssueDate, &docIssuePlace,
			&docFrontURL, &docBackURL,
			&docPortraitURL,
			&docStatus, &docRejection,
			&docWardID, &docAddressDetail,
		); err != nil {
			return nil, utils.Meta{}, fmt.Errorf("candidate: admin list scan: %w", err)
		}

		dto := ToAdminCandidateResponseDTO(c)
		if docID.Valid {
			d.CandidateDocumentID = docID.Int64
			d.CandidateDocumentCandidateID = docCandID.Int64
			d.CandidateDocumentType = enum.DocumentType(docType.String)
			d.CandidateDocumentNumber = docNumber.String
			d.CandidateDocumentIssueDate = docIssueDate.Time
			d.CandidateDocumentIssuePlace = docIssuePlace.String
			d.CandidateDocumentFrontImageURL = docFrontURL.String
			if docBackURL.Valid {
				d.CandidateDocumentBackImageURL = &docBackURL.String
			}
			d.CandidateDocumentPortraitImageURL = docPortraitURL.String
			d.CandidateDocumentVerificationStatus = enum.DocumentVerificationStatus(docStatus.String)
			if docRejection.Valid {
				d.CandidateDocumentRejectionReason = &docRejection.String
			}
			if docWardID.Valid {
				d.CandidateDocumentWardID = &docWardID.Int64
			}
			if docAddressDetail.Valid {
				d.CandidateDocumentAddressDetail = &docAddressDetail.String
			}
			docDTO := ToDocumentResponseDTO(d)
			dto.LatestDocument = &docDTO
		}
		items = append(items, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin list rows: %w", err)
	}

	totalPages := (int(total) + req.Limit - 1) / req.Limit
	return items, utils.Meta{
		Page:       req.Page,
		PerPage:    req.Limit,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// AdminGetCandidate returns the full candidate detail including latest document.
func (s *Service) AdminGetCandidate(ctx context.Context, candidateID int64) (response.AdminCandidateResponseDTO, error) {
	var c entities.CandidateEntity
	err := scanCandidate(s.db.QueryRowContext(ctx, `
		SELECT `+candidateSelectCols+`
		FROM candidates
		WHERE candidate_id = $1 AND candidate_is_deleted = FALSE`, candidateID,
	), &c)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AdminCandidateResponseDTO{}, errors.New("không tìm thấy hồ sơ")
	}
	if err != nil {
		return response.AdminCandidateResponseDTO{}, fmt.Errorf("candidate: admin get: %w", err)
	}

	dto := ToAdminCandidateResponseDTO(c)

	// Fetch latest document
	var d entities.CandidateDocumentEntity
	err = scanDocument(s.db.QueryRowContext(ctx, `
		SELECT `+docSelectCols+`
		FROM candidate_documents
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted = FALSE
		ORDER BY candidate_document_created_at DESC
		LIMIT 1`, candidateID,
	), &d)
	if err == nil {
		docDTO := ToDocumentResponseDTO(d)
		dto.LatestDocument = &docDTO
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response.AdminCandidateResponseDTO{}, fmt.Errorf("candidate: admin get doc: %w", err)
	}

	return dto, nil
}

// AdminApproveDocument sets the latest pending document status to 'verified'.
func (s *Service) AdminApproveDocument(ctx context.Context, adminID, candidateID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'verified',
		    candidate_document_rejection_reason    = NULL
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status = 'pending'
		  AND candidate_document_id = (
		      SELECT candidate_document_id FROM candidate_documents
		      WHERE candidate_document_candidate_id = $1
		        AND candidate_document_is_deleted = FALSE
		      ORDER BY candidate_document_created_at DESC
		      LIMIT 1
		  )`,
		candidateID,
	)
	if err != nil {
		return fmt.Errorf("candidate: approve document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không có hồ sơ đang chờ duyệt hoặc đã được xử lý")
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
		EntityID:       candidateID,
	})

	return tx.Commit()
}

// AdminRejectDocument sets the latest pending document status to 'rejected' with a reason.
func (s *Service) AdminRejectDocument(ctx context.Context, adminID, candidateID int64, req request.AdminRejectDocumentRequestDTO) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'rejected',
		    candidate_document_rejection_reason    = $2
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status = 'pending'
		  AND candidate_document_id = (
		      SELECT candidate_document_id FROM candidate_documents
		      WHERE candidate_document_candidate_id = $1
		        AND candidate_document_is_deleted = FALSE
		      ORDER BY candidate_document_created_at DESC
		      LIMIT 1
		  )`,
		candidateID, req.Reason,
	)
	if err != nil {
		return fmt.Errorf("candidate: reject document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không có hồ sơ đang chờ duyệt hoặc đã được xử lý")
	}

	newVal, _ := json.Marshal(map[string]any{"reason": req.Reason})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionReject.String(),
		EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
		EntityID:       candidateID,
		NewValue:       newVal,
	})

	return tx.Commit()
}

// AdminListDuplicates returns groups of candidates sharing the same active document number.
func (s *Service) AdminListDuplicates(ctx context.Context) ([]response.DuplicateDocResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate_document_type,
		       candidate_document_number,
		       ARRAY_AGG(DISTINCT candidate_document_candidate_id ORDER BY candidate_document_candidate_id) AS candidate_ids,
		       COUNT(DISTINCT candidate_document_candidate_id) AS cnt
		FROM candidate_documents
		WHERE candidate_document_is_deleted = FALSE
		GROUP BY candidate_document_type, candidate_document_number
		HAVING COUNT(DISTINCT candidate_document_candidate_id) > 1
		ORDER BY cnt DESC, candidate_document_number`,
	)
	if err != nil {
		return nil, fmt.Errorf("candidate: list duplicates: %w", err)
	}
	defer rows.Close()

	var result []response.DuplicateDocResponseDTO
	for rows.Next() {
		var (
			docType, docNumber string
			candidateIDs       []int64
			cnt                int
		)
		if err := rows.Scan(&docType, &docNumber, &candidateIDs, &cnt); err != nil {
			return nil, fmt.Errorf("candidate: duplicates scan: %w", err)
		}
		result = append(result, response.DuplicateDocResponseDTO{
			DocType:      docType,
			DocNumber:    docNumber,
			CandidateIDs: candidateIDs,
			Count:        cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate: duplicates rows: %w", err)
	}
	if result == nil {
		result = []response.DuplicateDocResponseDTO{}
	}
	return result, nil
}

// AdminGetRoster returns the attendance roster for an exam session, including portrait images.
func (s *Service) AdminGetRoster(ctx context.Context, examSessionID int64) ([]response.RosterItemResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT es.exam_seat_number,
		       c.candidate_id,
		       c.candidate_full_name,
		       c.candidate_dob,
		       c.candidate_gender,
		       c.candidate_nationality,
		       d.candidate_document_number,
		       d.candidate_document_type,
		       d.candidate_document_portrait_image_url
		FROM exam_registrations er
		JOIN candidates c
		  ON c.candidate_id = er.exam_registration_candidate_id
		  AND c.candidate_is_deleted = FALSE
		JOIN exam_seats es
		  ON es.exam_seat_id = er.exam_registration_exam_seat_id
		  AND es.exam_seat_is_deleted = FALSE
		LEFT JOIN LATERAL (
		    SELECT candidate_document_number,
		           candidate_document_type,
		           candidate_document_portrait_image_url
		    FROM candidate_documents
		    WHERE candidate_document_candidate_id = c.candidate_id
		      AND candidate_document_is_deleted   = FALSE
		      AND candidate_document_verification_status = 'verified'
		    ORDER BY candidate_document_created_at DESC
		    LIMIT 1
		) d ON TRUE
		WHERE er.exam_registration_exam_session_id = $1
		  AND er.exam_registration_status = 'confirmed'
		  AND er.exam_registration_is_deleted = FALSE
		ORDER BY es.exam_seat_number`,
		examSessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("candidate: get roster: %w", err)
	}
	defer rows.Close()

	var items []response.RosterItemResponseDTO
	for rows.Next() {
		var item response.RosterItemResponseDTO
		var (
			docNumber, docType, portraitURL sql.NullString
		)
		if err := rows.Scan(
			&item.SeatNumber, &item.CandidateID,
			&item.FullName, &item.DOB, &item.Gender, &item.Nationality,
			&docNumber, &docType, &portraitURL,
		); err != nil {
			return nil, fmt.Errorf("candidate: roster scan: %w", err)
		}
		item.DocNumber = docNumber.String
		item.DocType = docType.String
		item.PortraitImageURL = portraitURL.String
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate: roster rows: %w", err)
	}
	if items == nil {
		items = []response.RosterItemResponseDTO{}
	}
	return items, nil
}
