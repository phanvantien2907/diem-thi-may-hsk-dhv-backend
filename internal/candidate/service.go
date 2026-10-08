package candidate

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/candidate/enum"
	"github.com/hskvinh/backend/internal/utils"
)

type adminCandidateExportRow struct {
	Name         string
	DOB          time.Time
	CCCD         string
	Level        string
	RegisteredAt time.Time
	Status       string
	Note         string
}

// Noto Serif SC embeds Latin, Vietnamese, and Simplified Chinese glyphs.
//
//go:embed assets/NotoSerifSC.ttf
var candidatePDFUnicodeFont []byte

func adminCandidateWhere(req request.AdminListCandidatesRequestDTO, startArg int) (string, []any) {
	conditions := []string{"c.candidate_is_deleted = FALSE"}
	args := make([]any, 0, 4)
	nextArg := startArg
	if req.Status != "" && req.Status != "all" {
		conditions = append(conditions, fmt.Sprintf("COALESCE(ld.status, 'pending') = $%d", nextArg))
		args = append(args, req.Status)
		nextArg++
	}
	if req.Level != "" {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM exam_registrations er
			JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
			  AND es.exam_session_is_deleted = FALSE
			JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
			WHERE er.exam_registration_candidate_id = c.candidate_id
			  AND er.exam_registration_is_deleted = FALSE
			  AND et.exam_type_is_deleted = FALSE
			  AND et.exam_type_name ILIKE $%d
		)`, nextArg))
		args = append(args, "%"+req.Level+"%")
		nextArg++
	}
	if req.ExamDate != "" {
		if _, err := time.Parse("2006-01-02", req.ExamDate); err != nil {
			return "WHERE FALSE", nil
		}
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM exam_registrations der
			JOIN exam_sessions des ON des.exam_session_id = der.exam_registration_exam_session_id
			  AND des.exam_session_is_deleted = FALSE
			WHERE der.exam_registration_candidate_id = c.candidate_id
			  AND der.exam_registration_is_deleted = FALSE
			  AND des.exam_session_date = $%d
		)`, nextArg))
		args = append(args, req.ExamDate)
		nextArg++
	}
	if req.Search != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			c.candidate_full_name ILIKE $%d
			OR EXISTS (
				SELECT 1
				FROM candidate_documents sd
				WHERE sd.candidate_document_candidate_id = c.candidate_id
				  AND sd.candidate_document_is_deleted = FALSE
				  AND sd.candidate_document_number ILIKE $%d
			)
		)`, nextArg, nextArg))
		args = append(args, "%"+req.Search+"%")
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

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
	candidate_document_ward_id, (SELECT ward_province_id FROM wards WHERE ward_id = candidate_document_ward_id) AS candidate_document_province_id, candidate_document_address_detail`

func scanDocument(row *sql.Row, e *entities.CandidateDocumentEntity) error {
	return row.Scan(
		&e.CandidateDocumentID, &e.CandidateDocumentCandidateID,
		&e.CandidateDocumentType, &e.CandidateDocumentNumber,
		&e.CandidateDocumentIssueDate, &e.CandidateDocumentIssuePlace,
		&e.CandidateDocumentFrontImageURL, &e.CandidateDocumentBackImageURL,
		&e.CandidateDocumentPortraitImageURL,
		&e.CandidateDocumentVerificationStatus, &e.CandidateDocumentRejectionReason,
		&e.CandidateDocumentWardID, &e.CandidateDocumentProvinceID, &e.CandidateDocumentAddressDetail,
	)
}

// normalizeChineseName removes one accidental layer of JSON string quoting.
// This keeps compatibility with clients that JSON-encode the name twice while
// leaving normal names and names containing only one quote unchanged.
func normalizeChineseName(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
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

	req.FullNameCN = normalizeChineseName(req.FullNameCN)

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
			&d.CandidateDocumentWardID, &d.CandidateDocumentProvinceID, &d.CandidateDocumentAddressDetail,
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

	req.FullNameCN = normalizeChineseName(req.FullNameCN)

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
	return s.AdminListCandidatesFiltered(ctx, req)
}

// AdminListCandidatesFiltered returns the paginated admin candidate list with all filters.
func (s *Service) AdminListCandidatesFiltered(ctx context.Context, req request.AdminListCandidatesRequestDTO) ([]response.AdminCandidateResponseDTO, utils.Meta, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 20
	}
	where, filterArgs := adminCandidateWhere(req, 1)
	const latestDocumentCTE = `
			WITH latest_documents AS (
				SELECT DISTINCT ON (candidate_document_candidate_id)
					candidate_document_candidate_id AS candidate_id,
					candidate_document_id,
					candidate_document_type,
					candidate_document_number,
					candidate_document_issue_date,
					candidate_document_issue_place,
					candidate_document_front_image_url,
					candidate_document_back_image_url,
					candidate_document_portrait_image_url,
					candidate_document_verification_status AS status,
					candidate_document_rejection_reason,
					candidate_document_ward_id,
					(SELECT ward_province_id FROM wards WHERE ward_id = candidate_document_ward_id) AS province_id,
					candidate_document_address_detail
				FROM candidate_documents
				WHERE candidate_document_is_deleted = FALSE
				ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC
			),
			latest_registrations AS (
				SELECT DISTINCT ON (er.exam_registration_candidate_id)
					er.exam_registration_candidate_id AS candidate_id,
					et.exam_type_name
				FROM exam_registrations er
				JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
				  AND es.exam_session_is_deleted = FALSE
				JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
				  AND et.exam_type_is_deleted = FALSE
				WHERE er.exam_registration_is_deleted = FALSE
				ORDER BY er.exam_registration_candidate_id, er.exam_registration_registered_at DESC
			)`

	var total int64
	if err := s.db.QueryRowContext(ctx, latestDocumentCTE+`
			SELECT COUNT(*)
			FROM candidates c
			LEFT JOIN latest_documents ld ON ld.candidate_id = c.candidate_id
			`+where, filterArgs...).Scan(&total); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin filtered count: %w", err)
	}

	offset := (req.Page - 1) * req.Limit
	args := append(filterArgs, req.Limit, offset)
	rows, err := s.db.QueryContext(ctx, latestDocumentCTE+`
			SELECT c.candidate_id, c.candidate_account_id, c.candidate_full_name,
			       c.candidate_full_name_cn, c.candidate_dob, c.candidate_gender,
			       c.candidate_ethnicity, c.candidate_religion, c.candidate_birthplace,
			       c.candidate_nationality, c.candidate_mother_tongue,
			       c.candidate_chinese_study_years,
			       a.account_phone, a.account_email, lr.exam_type_name,
			       ld.candidate_document_id, ld.candidate_id, ld.candidate_document_type,
			       ld.candidate_document_number, ld.candidate_document_issue_date,
			       ld.candidate_document_issue_place, ld.candidate_document_front_image_url,
			       ld.candidate_document_back_image_url, ld.candidate_document_portrait_image_url,
			       ld.status, ld.candidate_document_rejection_reason, ld.candidate_document_ward_id,
			       ld.province_id, ld.candidate_document_address_detail
			FROM candidates c
			JOIN accounts a ON a.account_id = c.candidate_account_id
			  AND a.account_is_deleted = FALSE
			LEFT JOIN latest_documents ld ON ld.candidate_id = c.candidate_id
			LEFT JOIN latest_registrations lr ON lr.candidate_id = c.candidate_id
			`+where+`
			ORDER BY c.candidate_id DESC
			LIMIT $`+strconv.Itoa(len(filterArgs)+1)+` OFFSET $`+strconv.Itoa(len(filterArgs)+2), args...)
	if err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin filtered query: %w", err)
	}
	defer rows.Close()

	items := make([]response.AdminCandidateResponseDTO, 0, req.Limit)
	for rows.Next() {
		var c entities.CandidateEntity
		var docID, docCandidateID, docWardID, docProvinceID sql.NullInt64
		var docType, docNumber, docIssuePlace, docFrontURL, docBackURL, docPortraitURL sql.NullString
		var docIssueDate sql.NullTime
		var docStatus, rejectionReason sql.NullString
		var phone, email, examLevel, docAddressDetail sql.NullString
		if err := rows.Scan(
			&c.CandidateID, &c.CandidateAccountID, &c.CandidateFullName,
			&c.CandidateFullNameCN, &c.CandidateDOB, &c.CandidateGender,
			&c.CandidateEthnicity, &c.CandidateReligion, &c.CandidateBirthplace,
			&c.CandidateNationality, &c.CandidateMotherTongue,
			&c.CandidateChineseStudyYears, &phone, &email, &examLevel,
			&docID, &docCandidateID, &docType, &docNumber, &docIssueDate,
			&docIssuePlace, &docFrontURL, &docBackURL, &docPortraitURL,
			&docStatus, &rejectionReason, &docWardID, &docProvinceID, &docAddressDetail,
		); err != nil {
			return nil, utils.Meta{}, fmt.Errorf("candidate: admin filtered scan: %w", err)
		}
		dto := ToAdminCandidateResponseDTO(c)
		if phone.Valid {
			dto.Phone = &phone.String
		}
		if email.Valid {
			dto.Email = &email.String
		}
		if examLevel.Valid {
			dto.ExamLevel = &examLevel.String
			dto.TargetExamLevel = &examLevel.String
		}
		if docID.Valid {
			doc := entities.CandidateDocumentEntity{
				CandidateDocumentID:                 docID.Int64,
				CandidateDocumentCandidateID:        docCandidateID.Int64,
				CandidateDocumentType:               enum.DocumentType(docType.String),
				CandidateDocumentNumber:             docNumber.String,
				CandidateDocumentIssueDate:          docIssueDate.Time,
				CandidateDocumentIssuePlace:         docIssuePlace.String,
				CandidateDocumentFrontImageURL:      docFrontURL.String,
				CandidateDocumentPortraitImageURL:   docPortraitURL.String,
				CandidateDocumentVerificationStatus: enum.DocumentVerificationStatus(docStatus.String),
			}
			if docBackURL.Valid {
				doc.CandidateDocumentBackImageURL = &docBackURL.String
			}
			if docWardID.Valid {
				doc.CandidateDocumentWardID = &docWardID.Int64
			}
			if docProvinceID.Valid {
				doc.CandidateDocumentProvinceID = &docProvinceID.Int64
			}
			if docAddressDetail.Valid {
				doc.CandidateDocumentAddressDetail = &docAddressDetail.String
			}
			if rejectionReason.Valid {
				doc.CandidateDocumentRejectionReason = &rejectionReason.String
			}
			docDTO := ToAdminDocumentResponseDTO(doc)
			dto.LatestDocument = &docDTO
		}
		items = append(items, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, utils.Meta{}, fmt.Errorf("candidate: admin filtered rows: %w", err)
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(req.Limit) - 1) / int64(req.Limit))
	}
	return items, utils.Meta{Page: req.Page, PerPage: req.Limit, TotalItems: total, TotalPages: totalPages}, nil
}

// AdminCandidateMetrics returns the total and latest-document status counts.
func (s *Service) AdminCandidateMetrics(ctx context.Context) (response.AdminCandidateMetricsResponseDTO, error) {
	query := `
			WITH latest_documents AS (
				SELECT DISTINCT ON (candidate_document_candidate_id)
					candidate_document_candidate_id AS candidate_id,
					candidate_document_verification_status AS status
				FROM candidate_documents
				WHERE candidate_document_is_deleted = FALSE
				ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC
			)
			SELECT COUNT(*) AS total,
			       COUNT(*) FILTER (WHERE ld.status = 'pending' OR ld.status IS NULL) AS pending,
			       COUNT(*) FILTER (WHERE ld.status = 'verified') AS verified,
			       COUNT(*) FILTER (WHERE ld.status = 'rejected') AS rejected
			FROM candidates c
			LEFT JOIN latest_documents ld ON ld.candidate_id = c.candidate_id
			WHERE c.candidate_is_deleted = FALSE`
	var metrics response.AdminCandidateMetricsResponseDTO
	if err := s.db.QueryRowContext(ctx, query).Scan(&metrics.Total, &metrics.Pending, &metrics.Verified, &metrics.Rejected); err != nil {
		return response.AdminCandidateMetricsResponseDTO{}, fmt.Errorf("candidate: admin metrics: %w", err)
	}
	return metrics, nil
}

// AdminExportCandidates returns a compact PDF for the filtered candidate list.
func (s *Service) AdminExportCandidates(ctx context.Context, req request.AdminListCandidatesRequestDTO) ([]byte, error) {
	examDate, err := time.Parse("2006-01-02", req.ExamDate)
	if err != nil {
		return nil, errors.New("ngày thi không hợp lệ, định dạng đúng là YYYY-MM-DD")
	}
	filterReq := req
	filterReq.ExamDate = ""
	where, filterArgs := adminCandidateWhere(filterReq, 2)
	query := `
			WITH latest_documents AS (
				SELECT DISTINCT ON (candidate_document_candidate_id)
					candidate_document_candidate_id AS candidate_id,
					candidate_document_number,
					candidate_document_verification_status AS status,
					candidate_document_rejection_reason
				FROM candidate_documents
				WHERE candidate_document_is_deleted = FALSE
				ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC
			),
			batch_registrations AS (
				SELECT DISTINCT ON (er.exam_registration_candidate_id)
					er.exam_registration_candidate_id AS candidate_id,
					er.exam_registration_registered_at,
					et.exam_type_name
				FROM exam_registrations er
				JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
				JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
				WHERE er.exam_registration_is_deleted = FALSE
				  AND es.exam_session_is_deleted = FALSE
				  AND et.exam_type_is_deleted = FALSE
				  AND es.exam_session_date = $1
				  AND et.exam_type_name ~* 'HSK[[:space:]]*[3-9]'
				ORDER BY er.exam_registration_candidate_id, er.exam_registration_registered_at DESC
			)
			SELECT c.candidate_full_name, c.candidate_dob,
			       COALESCE(ld.candidate_document_number, ''),
			       br.exam_type_name, br.exam_registration_registered_at,
			       COALESCE(ld.status, 'pending'),
			       COALESCE(ld.candidate_document_rejection_reason, '')
			FROM candidates c
			JOIN batch_registrations br ON br.candidate_id = c.candidate_id
			LEFT JOIN latest_documents ld ON ld.candidate_id = c.candidate_id
			` + where + `
			ORDER BY c.candidate_full_name ASC, c.candidate_id ASC`
	args := append([]any{examDate}, filterArgs...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("candidate: admin export query: %w", err)
	}
	defer rows.Close()

	records := make([]adminCandidateExportRow, 0)
	for rows.Next() {
		var record adminCandidateExportRow
		if err := rows.Scan(&record.Name, &record.DOB, &record.CCCD, &record.Level, &record.RegisteredAt, &record.Status, &record.Note); err != nil {
			return nil, fmt.Errorf("candidate: admin export scan: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate: admin export rows: %w", err)
	}
	pdf, err := buildCandidatePDF(records, examDate)
	if err != nil {
		return nil, fmt.Errorf("candidate: build export pdf: %w", err)
	}
	return pdf, nil
}

func buildCandidatePDF(records []adminCandidateExportRow, examDate time.Time) ([]byte, error) {
	const rowsPerPage = 24
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(false, 10)
	pdf.AddUTF8FontFromBytes("NotoSerifSC", "", candidatePDFUnicodeFont)
	pdf.SetTitle("Danh sách thí sinh dự thi HSK", true)

	for start := 0; start < len(records) || start == 0; start += rowsPerPage {
		pdf.AddPage()
		pdf.SetFont("NotoSerifSC", "", 10)
		pdf.CellFormat(130, 5, "ĐẠI HỌC VINH", "", 0, "C", false, 0, "")
		pdf.CellFormat(137, 5, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "", 1, "C", false, 0, "")
		pdf.CellFormat(130, 5, "TRUNG TÂM KHẢO THÍ VÀ QUẢN LÝ CHẤT LƯỢNG GIÁO DỤC", "", 0, "C", false, 0, "")
		pdf.CellFormat(137, 5, "Độc lập - Tự do - Hạnh phúc", "", 1, "C", false, 0, "")
		pdf.Ln(8)
		pdf.SetFont("NotoSerifSC", "", 13)
		pdf.CellFormat(0, 7, "DANH SÁCH THÍ SINH DỰ THI HSK NGÀY "+examDate.Format("02/01/2006"), "", 1, "C", false, 0, "")
		pdf.Ln(5)

		widths := []float64{12, 45, 30, 35, 25, 42, 88}
		headers := []string{"STT", "Họ và tên", "Ngày sinh", "Số CCCD", "Cấp thi", "Ngày đăng ký", "Ghi chú"}
		pdf.SetFont("NotoSerifSC", "", 8)
		for i, header := range headers {
			pdf.CellFormat(widths[i], 9, header, "1", 0, "CM", false, 0, "")
		}
		pdf.Ln(-1)

		end := start + rowsPerPage
		if end > len(records) {
			end = len(records)
		}
		for i := start; i < end; i++ {
			record := records[i]
			note := record.Note
			if record.Status == "rejected" && note != "" {
				note = "Từ chối: " + note
			} else if note == "" {
				note = "-"
			}
			values := []string{
				strconv.Itoa(i + 1), record.Name, record.DOB.Format("02/01/2006"),
				record.CCCD, record.Level, record.RegisteredAt.Format("02/01/2006 15:04"), note,
			}
			for j, value := range values {
				pdf.CellFormat(widths[j], 8, fitPDFText(pdf, value, widths[j]-2), "1", 0, "LM", false, 0, "")
			}
			pdf.Ln(-1)
		}
	}
	if err := pdf.Error(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func fitPDFText(pdf *fpdf.Fpdf, value string, maxWidth float64) string {
	if pdf.GetStringWidth(value) <= maxWidth {
		return value
	}
	const suffix = "..."
	runes := []rune(value)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + suffix
		if pdf.GetStringWidth(candidate) <= maxWidth {
			return candidate
		}
	}
	return suffix
}

// AdminGetCandidate returns the full candidate detail including latest document.
func (s *Service) AdminGetCandidate(ctx context.Context, candidateID int64) (response.AdminCandidateResponseDTO, error) {
	var c entities.CandidateEntity
	var phone, email, examLevel sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT `+candidateSelectCols+`, a.account_phone, a.account_email,
		       (
		         SELECT et.exam_type_name
		         FROM exam_registrations er
		         JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		           AND es.exam_session_is_deleted = FALSE
		         JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		           AND et.exam_type_is_deleted = FALSE
		         WHERE er.exam_registration_candidate_id = c.candidate_id
		           AND er.exam_registration_is_deleted = FALSE
		         ORDER BY er.exam_registration_registered_at DESC
		         LIMIT 1
		       ) AS exam_type_name
		FROM candidates c
		JOIN accounts a ON a.account_id = c.candidate_account_id
		  AND a.account_is_deleted = FALSE
		WHERE c.candidate_id = $1 AND c.candidate_is_deleted = FALSE`, candidateID,
	).Scan(
		&c.CandidateID, &c.CandidateAccountID, &c.CandidateFullName, &c.CandidateFullNameCN,
		&c.CandidateDOB, &c.CandidateGender, &c.CandidateEthnicity, &c.CandidateReligion,
		&c.CandidateBirthplace, &c.CandidateNationality, &c.CandidateMotherTongue,
		&c.CandidateChineseStudyYears, &phone, &email, &examLevel,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AdminCandidateResponseDTO{}, errors.New("không tìm thấy hồ sơ")
	}
	if err != nil {
		return response.AdminCandidateResponseDTO{}, fmt.Errorf("candidate: admin get: %w", err)
	}

	dto := ToAdminCandidateResponseDTO(c)
	if phone.Valid {
		dto.Phone = &phone.String
	}
	if email.Valid {
		dto.Email = &email.String
	}
	if examLevel.Valid {
		dto.ExamLevel = &examLevel.String
		dto.TargetExamLevel = &examLevel.String
	}

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
		docDTO := ToAdminDocumentResponseDTO(d)
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
