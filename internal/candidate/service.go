package candidate

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/cache"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/dto/response"
	"github.com/hskvinh/backend/internal/candidate/entities"
	"github.com/hskvinh/backend/internal/candidate/enum"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
	"github.com/lib/pq"
)

const (
	cachePrefixCandidateProfile  = "hsk:cand:prof:acc:"
	cachePrefixCandidateDocs     = "hsk:cand:docs:acc:"
	cachePrefixCandidateSoftLock = "hsk:cand:softlock:"
	cachePrefixCandidateViewed   = "hsk:cand:viewed:"
	softLockTTL                  = 5 * time.Minute
	proofOfViewTTL               = 15 * time.Minute
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

// PDFUnicodeFont returns the embedded NotoSerifSC TrueType font bytes so other
// packages (such as internal/registration) can render UTF-8 Vietnamese/Chinese
// PDFs without duplicating the 25MB font in the compiled binary.
func PDFUnicodeFont() []byte {
	return candidatePDFUnicodeFont
}

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
	db            *sql.DB
	auditSvc      audit.Service
	mediaSvc      MediaService
	visionSvc     VisionService
	cacheSvc      *cache.Service
	reviewLocks   sync.Map // map[int64]softLockEntry (in-memory fallback when Redis is disabled)
	viewedByAdmin sync.Map // map[string]time.Time    (key: "<candidateID>:<adminID>")
}

type softLockEntry struct {
	AdminID     int64     `json:"admin_id"`
	LockedUntil time.Time `json:"locked_until"`
}

// SetCache injects the Redis cache service.
func (s *Service) SetCache(cacheSvc *cache.Service) {
	s.cacheSvc = cacheSvc
}

// InvalidateCandidateCache clears candidate profile and document caches by candidate ID.
// Ensures all code paths (candidate upload, admin review, admin reject) share a single invalidation logic.
func (s *Service) InvalidateCandidateCache(ctx context.Context, candidateID int64) {
	if s.cacheSvc == nil || candidateID <= 0 {
		return
	}
	var accID int64
	_ = s.db.QueryRowContext(ctx, `SELECT candidate_account_id FROM candidates WHERE candidate_id = $1`, candidateID).Scan(&accID)
	if accID > 0 {
		s.InvalidateCandidateCacheByAccountID(ctx, accID)
	}
}

// InvalidateCandidateCacheByAccountID clears candidate profile and document caches by account ID.
func (s *Service) InvalidateCandidateCacheByAccountID(ctx context.Context, accountID int64) {
	if s.cacheSvc == nil || accountID <= 0 {
		return
	}
	_ = s.cacheSvc.Delete(ctx,
		fmt.Sprintf("%s%d", cachePrefixCandidateProfile, accountID),
		fmt.Sprintf("%s%d", cachePrefixCandidateDocs, accountID),
	)
	_ = s.cacheSvc.DeletePrefix(ctx, "hsk:cand:admin:")
}

func (s *Service) invalidateCandidateDocs(ctx context.Context, candidateID int64) {
	s.InvalidateCandidateCache(ctx, candidateID)
}

// ClearMyCache clears Redis cache for the calling candidate (for manual FE refresh).
func (s *Service) ClearMyCache(ctx context.Context, accountID int64) error {
	s.InvalidateCandidateCacheByAccountID(ctx, accountID)
	return nil
}

// AdminClearCache clears all candidate caches across the system (for FE/Admin manual refresh).
func (s *Service) AdminClearCache(ctx context.Context) error {
	if s.cacheSvc == nil {
		return nil
	}
	return s.cacheSvc.DeletePrefix(ctx, "hsk:cand:")
}

// ErrActiveRegistration is returned when a candidate has an active registration
// and attempts to modify their profile.
var ErrActiveRegistration = errors.New("không thể sửa hồ sơ khi đã có đăng ký ca thi đang hoạt động")

// ErrCandidateProfileNotFound is returned when the account has no candidate profile.
var ErrCandidateProfileNotFound = errors.New("chưa có hồ sơ thí sinh")

// ErrInvalidIssueDate is returned when a document issue date is not YYYY-MM-DD.
var ErrInvalidIssueDate = errors.New("ngày cấp không hợp lệ (định dạng YYYY-MM-DD)")

// ErrDuplicateDocumentNumber is returned when a document number is already registered by another candidate.
var ErrDuplicateDocumentNumber = errors.New("Số giấy tờ này (CCCD/Hộ chiếu) đã được đăng ký bởi một thí sinh khác trên hệ thống")

// ErrCandidateProfileAlreadyExists is returned when attempting to create a profile for an account that already has one.
var ErrCandidateProfileAlreadyExists = errors.New("Hồ sơ thí sinh cho tài khoản này đã tồn tại")

// ErrCandidateNotFound is returned when a candidate profile cannot be found.
var ErrCandidateNotFound = errors.New("không tìm thấy hồ sơ")

// ErrDocumentStateConflict is returned when optimistic locking detects a concurrent state change (0 rows affected).
var ErrDocumentStateConflict = errors.New("không có hồ sơ ở trạng thái hợp lệ hoặc hồ sơ đã được quản trị viên khác xử lý")

// ErrRejectToApproveForbidden is returned when an admin attempts to directly approve a rejected profile without re-opening review.
var ErrRejectToApproveForbidden = errors.New("hồ sơ đang ở trạng thái từ chối; không thể phê duyệt trực tiếp — thí sinh phải nộp lại hoặc quản trị viên phải mở lại xét duyệt")

// ErrApprovalRequiresDetailInspection is returned when an admin attempts to approve from the list view without opening the detail view first.
var ErrApprovalRequiresDetailInspection = errors.New("vui lòng mở xem chi tiết hồ sơ và kiểm tra ảnh giấy tờ tùy thân trước khi phê duyệt")

// ErrMissingIdentityPhotos is returned when a candidate document lacks required identity images.
var ErrMissingIdentityPhotos = errors.New("hồ sơ thiếu ảnh giấy tờ tùy thân hoặc ảnh chân dung bắt buộc")

// ErrDuplicateDocumentBlocker is returned when approving a document whose number collides with another active candidate.
var ErrDuplicateDocumentBlocker = errors.New("số giấy tờ tùy thân bị trùng với hồ sơ thí sinh khác; vui lòng xử lý gộp hồ sơ trùng lặp trước khi phê duyệt")

// ErrProfileLockedByAnotherAdmin is returned when another admin holds an active 5-minute soft lock on the candidate profile.
var ErrProfileLockedByAnotherAdmin = errors.New("hồ sơ đang được quản trị viên khác xem và xử lý")

// ErrRevokeBlockedByPaidRegistration is returned when revoking approval for a candidate who has a confirmed (paid) exam registration.
var ErrRevokeBlockedByPaidRegistration = errors.New("không thể thu hồi phê duyệt vì thí sinh đang có đơn đăng ký thi đã thanh toán; vui lòng chọn phương án hoàn tiền (paid_resolution=refund) nếu muốn tiếp tục")

// ErrInvalidMergeTargets is returned when primary and duplicate candidate IDs are invalid or identical.
var ErrInvalidMergeTargets = errors.New("ID hồ sơ chính và hồ sơ trùng lặp không hợp lệ hoặc trùng nhau")

// ErrMergeConflictConfirmedSameSession is returned when both candidates have paid registrations in the same exam session.
var ErrMergeConflictConfirmedSameSession = errors.New("không thể gộp tự động vì cả 2 hồ sơ đều có đơn đăng ký đã thanh toán trong cùng một ca thi")

// PortraitRejectedError is returned when the portrait image violates the photo
// rules. The upload is blocked and nothing is persisted.
type PortraitRejectedError struct {
	Reasons []string
}

func (e *PortraitRejectedError) Error() string {
	return "Ảnh chân dung không hợp lệ: " + strings.Join(e.Reasons, " ")
}

// NewService creates a candidate Service.
func NewService(db *sql.DB, auditSvc audit.Service, mediaSvc MediaService, visionSvc VisionService) *Service {
	return &Service{db: db, auditSvc: auditSvc, mediaSvc: mediaSvc, visionSvc: visionSvc}
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
		if strings.Contains(err.Error(), "uq_candidates_account") {
			return response.CandidateResponseDTO{}, ErrCandidateProfileAlreadyExists
		}
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
	s.InvalidateCandidateCacheByAccountID(ctx, accountID)
	return ToCandidateResponseDTO(e), nil
}

// GetMyProfile returns the candidate profile for the authenticated account.
func (s *Service) GetMyProfile(ctx context.Context, accountID int64) (response.CandidateResponseDTO, error) {
	cacheKey := fmt.Sprintf("%s%d", cachePrefixCandidateProfile, accountID)
	if s.cacheSvc != nil {
		var cached response.CandidateResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	var e entities.CandidateEntity
	err := scanCandidate(s.db.QueryRowContext(ctx, `
		SELECT `+candidateSelectCols+`
		FROM candidates
		WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		// Do NOT negative-cache /me/* endpoints to prevent race conditions right after profile creation
		return response.CandidateResponseDTO{}, errors.New("chưa có hồ sơ thí sinh")
	}
	if err != nil {
		return response.CandidateResponseDTO{}, fmt.Errorf("candidate: get profile: %w", err)
	}
	dto := ToCandidateResponseDTO(e)
	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, dto, 30*time.Minute)
	}
	return dto, nil
}

// GetMyDocuments returns all non-deleted identity documents for the authenticated account.
func (s *Service) GetMyDocuments(ctx context.Context, accountID int64) ([]entities.CandidateDocumentEntity, error) {
	cacheKey := fmt.Sprintf("%s%d", cachePrefixCandidateDocs, accountID)
	if s.cacheSvc != nil {
		var cached []entities.CandidateDocumentEntity
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

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
	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, docs, 30*time.Minute)
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
	s.InvalidateCandidateCacheByAccountID(ctx, accountID)
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
//
// The portrait image is validated BEFORE anything is persisted:
//   - invalid portrait  → *PortraitRejectedError, upload blocked, no row written
//   - valid (Vision)    → stored as 'verified'
//   - valid (local only, Vision not configured) or validator infra error
//     → stored as 'pending' for manual admin review
func (s *Service) UploadDocument(ctx context.Context, accountID int64, req request.UploadDocumentRequestDTO) (response.DocumentResponseDTO, error) {
	// Resolve candidate_id from accountID (authoritative ownership check)
	var candidateID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT candidate_id FROM candidates WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE`,
		accountID,
	).Scan(&candidateID); errors.Is(err, sql.ErrNoRows) {
		return response.DocumentResponseDTO{}, ErrCandidateProfileNotFound
	} else if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: lookup: %w", err)
	}

	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		return response.DocumentResponseDTO{}, ErrInvalidIssueDate
	}

	// Prevent duplicate document numbers: check if another candidate already registered with this document number
	var existingDocCandidateID int64
	err = s.db.QueryRowContext(ctx, `
		SELECT candidate_document_candidate_id
		FROM candidate_documents
		WHERE candidate_document_number = $1
		  AND candidate_document_candidate_id != $2
		  AND candidate_document_is_deleted = FALSE
		LIMIT 1`,
		req.DocNumber, candidateID,
	).Scan(&existingDocCandidateID)
	if err == nil {
		return response.DocumentResponseDTO{}, ErrDuplicateDocumentNumber
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: check duplicate doc: %w", err)
	}

	// Check if this document number is already registered as another candidate account's CCCD username
	var conflictingAccountID int64
	err = s.db.QueryRowContext(ctx, `
		SELECT a.account_id
		FROM accounts a
		JOIN candidates c ON c.candidate_account_id = a.account_id
		WHERE a.account_username = $1
		  AND c.candidate_id != $2
		  AND a.account_is_deleted = FALSE
		  AND c.candidate_is_deleted = FALSE
		LIMIT 1`,
		req.DocNumber, candidateID,
	).Scan(&conflictingAccountID)
	if err == nil {
		return response.DocumentResponseDTO{}, ErrDuplicateDocumentNumber
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: check duplicate account username: %w", err)
	}

	// Validate the portrait outside the transaction (network call) and fail fast.
	verificationStatus, err := s.validatePortrait(ctx, candidateID, req.PortraitImageURL)
	if err != nil {
		return response.DocumentResponseDTO{}, err
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
		        candidate_document_address_detail, candidate_document_verification_status)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,NULLIF($10,''), $11)
		RETURNING `+docSelectCols,
		candidateID, enum.DocumentType(req.DocType), req.DocNumber,
		issueDate, req.IssuePlace,
		req.FrontImageURL, req.BackImageURL, req.PortraitImageURL,
		req.WardID, req.AddressDetail, verificationStatus.String(),
	), &e)
	if err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: upload document: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"doc_type": string(e.CandidateDocumentType), "doc_number": e.CandidateDocumentNumber, "verification_status": e.CandidateDocumentVerificationStatus})
	if s.auditSvc != nil {
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &accountID,
			Action:         auditenum.AuditActionCreate.String(),
			EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
			EntityID:       e.CandidateDocumentID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return response.DocumentResponseDTO{}, fmt.Errorf("candidate: commit: %w", err)
	}
	s.InvalidateCandidateCacheByAccountID(ctx, accountID)
	return ToDocumentResponseDTO(e), nil
}

// validatePortrait runs the portrait validator and maps its outcome to the
// verification status to persist. Invalid portraits return *PortraitRejectedError.
func (s *Service) validatePortrait(ctx context.Context, candidateID int64, portraitURL string) (enum.DocumentVerificationStatus, error) {
	if s.visionSvc == nil {
		return enum.DocVerificationPending, nil
	}
	result, err := s.visionSvc.ValidatePortraitImage(ctx, portraitURL)
	if err != nil {
		// Infrastructure failure (network, Vision API): degrade to manual review.
		log.Printf("[candidate] portrait validation unavailable for candidate %d: %v", candidateID, err)
		return enum.DocVerificationPending, nil
	}
	if result == nil {
		return enum.DocVerificationPending, nil
	}
	if !result.Valid {
		reasons := result.Reasons
		if len(reasons) == 0 && result.RejectionReason != "" {
			reasons = []string{result.RejectionReason}
		}
		return "", &PortraitRejectedError{Reasons: reasons}
	}
	if result.ManualReviewRequired {
		return enum.DocVerificationPending, nil
	}
	return enum.DocVerificationVerified, nil
}

// ValidatePortrait inspects a portrait image URL and returns the detailed validation results.
// This allows the front-end to pre-validate the portrait immediately upon file selection.
func (s *Service) ValidatePortrait(ctx context.Context, req request.ValidatePortraitRequestDTO) (response.PortraitValidationResponseDTO, error) {
	if s.visionSvc == nil {
		return response.PortraitValidationResponseDTO{
			Valid:   true,
			Reasons: []string{},
		}, nil
	}
	result, err := s.visionSvc.ValidatePortraitImage(ctx, req.PortraitImageURL)
	if err != nil {
		log.Printf("[candidate] validate portrait image error: %v", err)
		return response.PortraitValidationResponseDTO{
			Valid:   true,
			Reasons: []string{},
		}, nil
	}
	return ToPortraitValidationResponseDTO(result), nil
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
	cacheKey := fmt.Sprintf("hsk:cand:admin:list:v2:%d:%d:%s:%s:%s:%s",
		req.Page, req.Limit, req.Status, req.Level, req.ExamDate, req.Search)
	if s.cacheSvc != nil {
		var cached struct {
			Items []response.AdminCandidateResponseDTO `json:"items"`
			Meta  utils.Meta                           `json:"meta"`
		}
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached.Items, cached.Meta, nil
		}
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
			maskedPhone := MaskPhoneNumber(phone.String)
			dto.Phone = &maskedPhone
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
			docDTO := ToAdminMaskedDocumentResponseDTO(doc)
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
	meta := utils.Meta{Page: req.Page, PerPage: req.Limit, TotalItems: total, TotalPages: totalPages}
	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, struct {
			Items []response.AdminCandidateResponseDTO `json:"items"`
			Meta  utils.Meta                           `json:"meta"`
		}{Items: items, Meta: meta}, 5*time.Minute)
	}
	return items, meta, nil
}

// AdminCandidateMetrics returns the total and latest-document status counts.
func (s *Service) AdminCandidateMetrics(ctx context.Context) (response.AdminCandidateMetricsResponseDTO, error) {
	const cacheKey = "hsk:cand:admin:metrics"
	if s.cacheSvc != nil {
		var cached response.AdminCandidateMetricsResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

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

	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, metrics, 5*time.Minute)
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

// acquireOrInspectSoftLock acquires or extends a 5-minute soft lock for adminID on candidateID,
// or returns the existing lock held by another admin.
func (s *Service) acquireOrInspectSoftLock(ctx context.Context, candidateID, adminID int64) *response.CandidateReviewLockDTO {
	if adminID <= 0 || candidateID <= 0 {
		return nil
	}
	now := time.Now().UTC()
	lockKey := fmt.Sprintf("%s%d", cachePrefixCandidateSoftLock, candidateID)

	// 1. Check Redis first if enabled
	if s.cacheSvc != nil && s.cacheSvc.Enabled() {
		var existing softLockEntry
		if hit, _ := s.cacheSvc.Get(ctx, lockKey, &existing); hit && existing.LockedUntil.After(now) {
			if existing.AdminID != adminID {
				return &response.CandidateReviewLockDTO{
					LockedByAccountID: existing.AdminID,
					LockedUntil:       existing.LockedUntil,
					IsLockedByOther:   true,
				}
			}
		}
		newEntry := softLockEntry{
			AdminID:     adminID,
			LockedUntil: now.Add(softLockTTL),
		}
		_ = s.cacheSvc.SetWithJitter(ctx, lockKey, newEntry, softLockTTL, 0.01)
		s.reviewLocks.Store(candidateID, newEntry)
		return &response.CandidateReviewLockDTO{
			LockedByAccountID: adminID,
			LockedUntil:       newEntry.LockedUntil,
			IsLockedByOther:   false,
		}
	}

	// 2. Fallback to in-memory lock tracker
	if raw, ok := s.reviewLocks.Load(candidateID); ok {
		existing := raw.(softLockEntry)
		if existing.LockedUntil.After(now) && existing.AdminID != adminID {
			return &response.CandidateReviewLockDTO{
				LockedByAccountID: existing.AdminID,
				LockedUntil:       existing.LockedUntil,
				IsLockedByOther:   true,
			}
		}
	}
	newEntry := softLockEntry{
		AdminID:     adminID,
		LockedUntil: now.Add(softLockTTL),
	}
	s.reviewLocks.Store(candidateID, newEntry)
	return &response.CandidateReviewLockDTO{
		LockedByAccountID: adminID,
		LockedUntil:       newEntry.LockedUntil,
		IsLockedByOther:   false,
	}
}

// checkSoftLock verifies that candidateID is not currently soft-locked by another admin.
func (s *Service) checkSoftLock(ctx context.Context, candidateID, adminID int64) error {
	if candidateID <= 0 || adminID <= 0 {
		return nil
	}
	now := time.Now().UTC()
	lockKey := fmt.Sprintf("%s%d", cachePrefixCandidateSoftLock, candidateID)

	if s.cacheSvc != nil && s.cacheSvc.Enabled() {
		var existing softLockEntry
		if hit, _ := s.cacheSvc.Get(ctx, lockKey, &existing); hit && existing.LockedUntil.After(now) && existing.AdminID != adminID {
			return ErrProfileLockedByAnotherAdmin
		}
	}
	if raw, ok := s.reviewLocks.Load(candidateID); ok {
		existing := raw.(softLockEntry)
		if existing.LockedUntil.After(now) && existing.AdminID != adminID {
			return ErrProfileLockedByAnotherAdmin
		}
	}
	return nil
}

// AdminReleaseCandidateLock releases the soft lock on candidateID if held by adminID.
func (s *Service) AdminReleaseCandidateLock(ctx context.Context, candidateID, adminID int64) error {
	if err := s.checkSoftLock(ctx, candidateID, adminID); err != nil {
		return err
	}
	lockKey := fmt.Sprintf("%s%d", cachePrefixCandidateSoftLock, candidateID)
	if s.cacheSvc != nil && s.cacheSvc.Enabled() {
		_ = s.cacheSvc.Delete(ctx, lockKey)
	}
	s.reviewLocks.Delete(candidateID)
	return nil
}

// recordProofOfView marks that adminID has opened and inspected the candidate detail and identity photos.
func (s *Service) recordProofOfView(ctx context.Context, candidateID, adminID int64) {
	if candidateID <= 0 || adminID <= 0 {
		return
	}
	exp := time.Now().UTC().Add(proofOfViewTTL)
	viewKey := fmt.Sprintf("%s%d:%d", cachePrefixCandidateViewed, candidateID, adminID)
	if s.cacheSvc != nil && s.cacheSvc.Enabled() {
		_ = s.cacheSvc.SetWithJitter(ctx, viewKey, exp, proofOfViewTTL, 0.01)
	}
	s.viewedByAdmin.Store(fmt.Sprintf("%d:%d", candidateID, adminID), exp)
}

// hasAdminViewedCandidate returns true if adminID inspected candidateID within the proof-of-view window.
func (s *Service) hasAdminViewedCandidate(ctx context.Context, candidateID, adminID int64) bool {
	if candidateID <= 0 || adminID <= 0 {
		return false
	}
	now := time.Now().UTC()
	if raw, ok := s.viewedByAdmin.Load(fmt.Sprintf("%d:%d", candidateID, adminID)); ok {
		if exp, valid := raw.(time.Time); valid && exp.After(now) {
			return true
		}
	}
	if s.cacheSvc != nil && s.cacheSvc.Enabled() {
		viewKey := fmt.Sprintf("%s%d:%d", cachePrefixCandidateViewed, candidateID, adminID)
		var exp time.Time
		if hit, _ := s.cacheSvc.Get(ctx, viewKey, &exp); hit && exp.After(now) {
			return true
		}
	}
	return false
}

// AdminGetCandidate returns the full unmasked candidate detail including latest document,
// acquires a 5-minute soft lock, records proof-of-view for approval gatekeeping, and writes a mandatory VIEW_PII audit log (Decree 13/2023).
func (s *Service) AdminGetCandidate(ctx context.Context, candidateID int64, adminID ...int64) (response.AdminCandidateResponseDTO, error) {
	var actorID int64
	if len(adminID) > 0 {
		actorID = adminID[0]
	}

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
		return response.AdminCandidateResponseDTO{}, ErrCandidateNotFound
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

	// Fetch latest document (unmasked for detail view)
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

	if actorID > 0 {
		dto.ReviewLock = s.acquireOrInspectSoftLock(ctx, candidateID, actorID)
		if dto.LatestDocument != nil &&
			strings.TrimSpace(dto.LatestDocument.FrontImageURL) != "" &&
			strings.TrimSpace(dto.LatestDocument.PortraitImageURL) != "" {
			s.recordProofOfView(ctx, candidateID, actorID)
		}

		// Decree 13/2023 Compliance: Mandatory Audit Trail for every unmasked PII access
		if s.auditSvc != nil {
			accessedFields := []string{"full_name", "dob", "phone", "email"}
			if dto.LatestDocument != nil {
				accessedFields = append(accessedFields, "doc_number", "front_image_url", "back_image_url", "portrait_image_url")
			}
			newVal, _ := json.Marshal(map[string]any{
				"accessed_fields": accessedFields,
			})
			_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
				ActorAccountID: &actorID,
				Action:         auditenum.AuditActionViewPII.String(),
				EntityTable:    auditenum.AuditEntityCandidate.String(),
				EntityID:       candidateID,
				NewValue:       newVal,
			})
		}
	}

	return dto, nil
}

// AdminApproveDocument sets the latest pending document status to 'verified'.
// Enforces:
//  1. 5-minute Soft-Lock ownership check (prevents concurrent admin collisions).
//  2. Approval Gatekeeping (Proof-of-View: admin must have inspected the detail view and photos first).
//  3. Strict State Machine (only 'pending' -> 'verified'; 'rejected' -> 'verified' is forbidden).
//  4. Hard-Blocker Duplicate ID Validation (fails with 409 if another active candidate holds the same document number).
//  5. Conditional Optimistic Locking (WHERE id = $1 AND status = 'pending').
func (s *Service) AdminApproveDocument(ctx context.Context, adminID, candidateID int64) error {
	if err := s.checkSoftLock(ctx, candidateID, adminID); err != nil {
		return err
	}
	if !s.hasAdminViewedCandidate(ctx, candidateID, adminID) {
		return ErrApprovalRequiresDetailInspection
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. Lock and inspect the latest active document
	var (
		docID       int64
		docType     string
		docNumber   string
		frontURL    string
		backURL     sql.NullString
		portraitURL string
		status      string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT candidate_document_id,
		       candidate_document_type,
		       candidate_document_number,
		       candidate_document_front_image_url,
		       candidate_document_back_image_url,
		       candidate_document_portrait_image_url,
		       candidate_document_verification_status
		FROM candidate_documents
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted = FALSE
		ORDER BY candidate_document_created_at DESC, candidate_document_id DESC
		LIMIT 1
		FOR UPDATE`,
		candidateID,
	).Scan(&docID, &docType, &docNumber, &frontURL, &backURL, &portraitURL, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDocumentStateConflict
	}
	if err != nil {
		return fmt.Errorf("candidate: lock document for approve: %w", err)
	}

	if status == string(enum.DocVerificationRejected) {
		return ErrRejectToApproveForbidden
	}
	if status != string(enum.DocVerificationPending) {
		return ErrDocumentStateConflict
	}
	if strings.TrimSpace(frontURL) == "" || strings.TrimSpace(portraitURL) == "" {
		return ErrMissingIdentityPhotos
	}

	// 2. Hard-Blocker Duplicate ID Validation (Step 3)
	var hasDuplicate bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM candidate_documents cd
			JOIN candidates c ON c.candidate_id = cd.candidate_document_candidate_id
			WHERE cd.candidate_document_number = $1
			  AND cd.candidate_document_candidate_id <> $2
			  AND cd.candidate_document_is_deleted = FALSE
			  AND c.candidate_is_deleted = FALSE
			UNION ALL
			SELECT 1
			FROM accounts a
			JOIN candidates c ON c.candidate_account_id = a.account_id
			WHERE a.account_username = $1
			  AND c.candidate_id <> $2
			  AND a.account_is_deleted = FALSE
			  AND c.candidate_is_deleted = FALSE
		)`,
		docNumber, candidateID,
	).Scan(&hasDuplicate)
	if err != nil {
		return fmt.Errorf("candidate: check duplicate before approve: %w", err)
	}
	if hasDuplicate {
		return ErrDuplicateDocumentBlocker
	}

	// 3. Optimistic Lock Conditional Update
	res, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'verified',
		    candidate_document_rejection_reason    = NULL
		WHERE candidate_document_id = $1
		  AND candidate_document_candidate_id = $2
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status = 'pending'`,
		docID, candidateID,
	)
	if err != nil {
		return fmt.Errorf("candidate: approve document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDocumentStateConflict
	}

	if s.auditSvc != nil {
		newVal, _ := json.Marshal(map[string]any{
			"document_id":         docID,
			"verification_status": "verified",
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionApprove.String(),
			EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
			EntityID:       candidateID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AdminReleaseCandidateLock(ctx, candidateID, adminID)
	s.invalidateCandidateDocs(ctx, candidateID)
	return nil
}

// AdminRejectDocument sets the latest pending document status to 'rejected' with a reason.
func (s *Service) AdminRejectDocument(ctx context.Context, adminID, candidateID int64, req request.AdminRejectDocumentRequestDTO) error {
	if err := s.checkSoftLock(ctx, candidateID, adminID); err != nil {
		return err
	}

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
		      ORDER BY candidate_document_created_at DESC, candidate_document_id DESC
		      LIMIT 1
		  )`,
		candidateID, req.Reason,
	)
	if err != nil {
		return fmt.Errorf("candidate: reject document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDocumentStateConflict
	}

	if s.auditSvc != nil {
		newVal, _ := json.Marshal(map[string]any{"reason": req.Reason})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionReject.String(),
			EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
			EntityID:       candidateID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AdminReleaseCandidateLock(ctx, candidateID, adminID)
	s.invalidateCandidateDocs(ctx, candidateID)
	return nil
}

// AdminRevokeApproval revokes a previously verified candidate profile ('verified' -> 'rejected')
// and cascades to active exam registrations so no unpaid or paid seats are left orphaned.
func (s *Service) AdminRevokeApproval(ctx context.Context, adminID, candidateID int64, req request.AdminRevokeApprovalRequestDTO) error {
	if err := s.checkSoftLock(ctx, candidateID, adminID); err != nil {
		return err
	}
	paidResolution := strings.TrimSpace(req.PaidResolution)
	if paidResolution == "" {
		paidResolution = "block"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. Optimistic Lock: only transition 'verified' -> 'rejected'
	res, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'rejected',
		    candidate_document_rejection_reason    = $2
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status = 'verified'
		  AND candidate_document_id = (
		      SELECT candidate_document_id FROM candidate_documents
		      WHERE candidate_document_candidate_id = $1
		        AND candidate_document_is_deleted = FALSE
		      ORDER BY candidate_document_created_at DESC, candidate_document_id DESC
		      LIMIT 1
		  )`,
		candidateID, req.Reason,
	)
	if err != nil {
		return fmt.Errorf("candidate: revoke approval update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDocumentStateConflict
	}

	// 2. Inspect and cascade active registrations (holding unpaid/paid seats)
	rows, err := tx.QueryContext(ctx, `
		SELECT er.exam_registration_id, er.exam_registration_exam_seat_id, er.exam_registration_status
		FROM exam_registrations er
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		WHERE er.exam_registration_candidate_id = $1
		  AND er.exam_registration_status IN ('pending_payment', 'confirmed')
		  AND er.exam_registration_is_deleted = FALSE
		  AND es.exam_session_status NOT IN ('completed', 'cancelled')
		ORDER BY er.exam_registration_id ASC
		FOR UPDATE OF er`,
		candidateID,
	)
	if err != nil {
		return fmt.Errorf("candidate: query active registrations on revoke: %w", err)
	}

	type activeReg struct {
		regID  int64
		seatID int64
		status string
	}
	var activeRegs []activeReg
	for rows.Next() {
		var ar activeReg
		if err := rows.Scan(&ar.regID, &ar.seatID, &ar.status); err != nil {
			rows.Close()
			return fmt.Errorf("candidate: scan active registration: %w", err)
		}
		activeRegs = append(activeRegs, ar)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("candidate: active registrations rows: %w", err)
	}
	rows.Close()

	var cancelledUnpaidIDs []int64
	var refundedPaidIDs []int64

	for _, ar := range activeRegs {
		if ar.status == "confirmed" && paidResolution != "refund" {
			return ErrRevokeBlockedByPaidRegistration
		}
	}

	note := "Thu hồi phê duyệt hồ sơ: " + req.Reason
	for _, ar := range activeRegs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE exam_registrations
			SET exam_registration_status = 'cancelled',
			    exam_registration_updated_at = NOW()
			WHERE exam_registration_id = $1`,
			ar.regID,
		); err != nil {
			return fmt.Errorf("candidate: cancel registration %d: %w", ar.regID, err)
		}

		if ar.status == "pending_payment" {
			_, _ = tx.ExecContext(ctx, `
				UPDATE payments
				SET payment_status = 'cancelled',
				    payment_admin_note = $2,
				    payment_updated_at = NOW()
				WHERE payment_exam_registration_id = $1
				  AND payment_status = 'pending'
				  AND payment_is_deleted = FALSE`,
				ar.regID, note,
			)
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_seats
				SET exam_seat_status = 'available',
				    exam_seat_held_until = NULL,
				    exam_seat_updated_at = NOW()
				WHERE exam_seat_id = $1
				  AND exam_seat_status = 'held'`,
				ar.seatID,
			)
			cancelledUnpaidIDs = append(cancelledUnpaidIDs, ar.regID)
		} else if ar.status == "confirmed" {
			_, _ = tx.ExecContext(ctx, `
				UPDATE payments
				SET payment_status = 'pending_refund',
				    payment_refund_reason = $2,
				    payment_updated_at = NOW()
				WHERE payment_exam_registration_id = $1
				  AND payment_status = 'success'
				  AND payment_is_deleted = FALSE`,
				ar.regID, note,
			)
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_seats
				SET exam_seat_status = 'available',
				    exam_seat_held_until = NULL,
				    exam_seat_updated_at = NOW()
				WHERE exam_seat_id = $1
				  AND exam_seat_status IN ('booked', 'held')`,
				ar.seatID,
			)
			refundedPaidIDs = append(refundedPaidIDs, ar.regID)
		}
	}

	if s.auditSvc != nil {
		oldVal, _ := json.Marshal(map[string]any{"verification_status": "verified"})
		newVal, _ := json.Marshal(map[string]any{
			"verification_status":               "rejected",
			"reason":                            req.Reason,
			"paid_resolution":                   paidResolution,
			"cancelled_unpaid_registration_ids": cancelledUnpaidIDs,
			"refunded_paid_registration_ids":    refundedPaidIDs,
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionRevokeApproval.String(),
			EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
			EntityID:       candidateID,
			OldValue:       oldVal,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AdminReleaseCandidateLock(ctx, candidateID, adminID)
	s.invalidateCandidateDocs(ctx, candidateID)
	return nil
}

// AdminReopenReview transitions a rejected candidate profile back to 'pending' ("Mở lại xét duyệt"),
// requiring an explicit reason and writing to the audit log.
func (s *Service) AdminReopenReview(ctx context.Context, adminID, candidateID int64, req request.AdminReopenReviewRequestDTO) error {
	if err := s.checkSoftLock(ctx, candidateID, adminID); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("candidate: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		SET candidate_document_verification_status = 'pending',
		    candidate_document_rejection_reason    = NULL
		WHERE candidate_document_candidate_id = $1
		  AND candidate_document_is_deleted   = FALSE
		  AND candidate_document_verification_status = 'rejected'
		  AND candidate_document_id = (
		      SELECT candidate_document_id FROM candidate_documents
		      WHERE candidate_document_candidate_id = $1
		        AND candidate_document_is_deleted = FALSE
		      ORDER BY candidate_document_created_at DESC, candidate_document_id DESC
		      LIMIT 1
		  )`,
		candidateID,
	)
	if err != nil {
		return fmt.Errorf("candidate: reopen review update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDocumentStateConflict
	}

	if s.auditSvc != nil {
		oldVal, _ := json.Marshal(map[string]any{"verification_status": "rejected"})
		newVal, _ := json.Marshal(map[string]any{
			"verification_status": "pending",
			"reason":              req.Reason,
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionReopenReview.String(),
			EntityTable:    auditenum.AuditEntityCandidateDocument.String(),
			EntityID:       candidateID,
			OldValue:       oldVal,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AdminReleaseCandidateLock(ctx, candidateID, adminID)
	s.invalidateCandidateDocs(ctx, candidateID)
	return nil
}

// AdminMergeCandidates invokes fn_merge_candidate_profiles to atomically merge a duplicate candidate
// into a primary candidate profile.
func (s *Service) AdminMergeCandidates(ctx context.Context, adminID int64, req request.AdminMergeCandidatesRequestDTO) (response.AdminMergeCandidatesResponseDTO, error) {
	if req.PrimaryCandidateID <= 0 || req.DuplicateCandidateID <= 0 || req.PrimaryCandidateID == req.DuplicateCandidateID {
		return response.AdminMergeCandidatesResponseDTO{}, ErrInvalidMergeTargets
	}
	if err := s.checkSoftLock(ctx, req.PrimaryCandidateID, adminID); err != nil {
		return response.AdminMergeCandidatesResponseDTO{}, err
	}
	if err := s.checkSoftLock(ctx, req.DuplicateCandidateID, adminID); err != nil {
		return response.AdminMergeCandidatesResponseDTO{}, err
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "Gộp hồ sơ thí sinh trùng lặp"
	}

	var rawJSON []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT fn_merge_candidate_profiles($1, $2, $3, $4)`,
		req.PrimaryCandidateID, req.DuplicateCandidateID, adminID, reason,
	).Scan(&rawJSON)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "INVALID_MERGE_TARGETS"):
			return response.AdminMergeCandidatesResponseDTO{}, ErrInvalidMergeTargets
		case strings.Contains(msg, "PRIMARY_CANDIDATE_NOT_FOUND"),
			strings.Contains(msg, "DUPLICATE_CANDIDATE_NOT_FOUND"),
			strings.Contains(msg, "CANDIDATE_NOT_FOUND"):
			return response.AdminMergeCandidatesResponseDTO{}, ErrCandidateNotFound
		case strings.Contains(msg, "MERGE_CONFLICT_BOTH_CONFIRMED_SAME_SESSION"):
			return response.AdminMergeCandidatesResponseDTO{}, ErrMergeConflictConfirmedSameSession
		default:
			return response.AdminMergeCandidatesResponseDTO{}, fmt.Errorf("candidate: merge profiles: %w", err)
		}
	}

	var result response.AdminMergeCandidatesResponseDTO
	if err := json.Unmarshal(rawJSON, &result); err != nil {
		return response.AdminMergeCandidatesResponseDTO{}, fmt.Errorf("candidate: unmarshal merge result: %w", err)
	}

	// Revoke active JWT tokens of the merged duplicate account in memory and Redis
	if result.DuplicateAccountID > 0 {
		middleware.RevokeAccountTokens(ctx, result.DuplicateAccountID, 24*time.Hour)
	}

	_ = s.AdminReleaseCandidateLock(ctx, req.PrimaryCandidateID, adminID)
	_ = s.AdminReleaseCandidateLock(ctx, req.DuplicateCandidateID, adminID)
	s.InvalidateCandidateCache(ctx, req.PrimaryCandidateID)
	s.InvalidateCandidateCache(ctx, req.DuplicateCandidateID)
	return result, nil
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
		if err := rows.Scan(&docType, &docNumber, pq.Array(&candidateIDs), &cnt); err != nil {
			return nil, fmt.Errorf("candidate: duplicates scan: %w", err)
		}
		result = append(result, response.DuplicateDocResponseDTO{
			DocType:      docType,
			DocNumber:    utils.MaskDocumentNumber(docNumber),
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
		item.DocNumber = utils.MaskDocumentNumber(docNumber.String)
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
