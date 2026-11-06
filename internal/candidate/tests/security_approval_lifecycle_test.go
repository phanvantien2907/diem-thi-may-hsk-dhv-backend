package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditres "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/notification"
	"github.com/hskvinh/backend/internal/utils/testutils"
)

type recordingNotifPublisher struct {
	mu     sync.Mutex
	events []notification.DomainEvent
}

func (p *recordingNotifPublisher) Publish(evt notification.DomainEvent) {
	p.mu.Lock()
	p.events = append(p.events, evt)
	p.mu.Unlock()
}

type recordingAuditService struct {
	mu   sync.Mutex
	logs []auditreq.CreateAuditLogRequestDTO
}

func (r *recordingAuditService) Log(ctx context.Context, tx *sql.Tx, req auditreq.CreateAuditLogRequestDTO) error {
	r.mu.Lock()
	r.logs = append(r.logs, req)
	r.mu.Unlock()
	return nil
}

func (r *recordingAuditService) List(ctx context.Context, req auditreq.ListAuditLogRequestDTO) (auditres.AuditLogListResponseDTO, error) {
	return auditres.AuditLogListResponseDTO{}, nil
}

func mockAdminGetCandidateQueries(mock sqlmock.Sqlmock, candidateID int64) {
	dob := time.Date(2000, 5, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .+ FROM candidates c JOIN accounts a`).
		WithArgs(candidateID).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_full_name_cn",
			"candidate_dob", "candidate_gender", "candidate_ethnicity", "candidate_religion",
			"candidate_birthplace", "candidate_nationality", "candidate_mother_tongue",
			"candidate_chinese_study_years", "account_phone", "account_email", "exam_type_name",
		}).AddRow(
			candidateID, int64(200), "Nguyễn Văn A", "阮文A",
			dob, "male", nil, nil, nil, "Việt Nam", nil, nil,
			"0901234567", "nguyenvana@example.com", "HSK 4",
		))

	mock.ExpectQuery(`SELECT .+ FROM candidate_documents WHERE candidate_document_candidate_id = \$1`).
		WithArgs(candidateID).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_candidate_id",
			"candidate_document_type", "candidate_document_number",
			"candidate_document_issue_date", "candidate_document_issue_place",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url",
			"candidate_document_verification_status", "candidate_document_rejection_reason",
			"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
		}).AddRow(
			int64(500), candidateID, "cccd", "040123456060", dob, "Cục CSQLHC",
			"https://cdn.example.com/front.jpg", "https://cdn.example.com/back.jpg",
			"https://cdn.example.com/portrait.jpg", "pending", nil, nil, nil, nil,
		))
}

// 1. Decree 13/2023 PII Masking & Audit Trail Tests
func TestMaskPII_Decree13Compliance(t *testing.T) {
	if got := candidate.MaskDocumentNumber("040123456060"); got != "040***060" {
		t.Fatalf("MaskDocumentNumber(040123456060) = %q, want 040***060", got)
	}
	if got := candidate.MaskPhoneNumber("0901234567"); got != "090***567" {
		t.Fatalf("MaskPhoneNumber(0901234567) = %q, want 090***567", got)
	}
	if got := candidate.MaskDocumentNumber("AB1234"); got != "A***4" {
		t.Fatalf("MaskDocumentNumber(AB1234) = %q, want A***4", got)
	}
}

func TestAdminGetCandidate_WritesViewPIIAuditLogAndAcquiresSoftLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	auditSvc := &recordingAuditService{}
	svc := candidate.NewService(db, auditSvc, nil, nil)

	mockAdminGetCandidateQueries(mock, 10)

	dto, err := svc.AdminGetCandidate(context.Background(), 10, 99)
	if err != nil {
		t.Fatalf("AdminGetCandidate() error = %v", err)
	}
	// Detail endpoint returns full unmasked PII
	if dto.Phone == nil || *dto.Phone != "0901234567" {
		t.Fatalf("expected unmasked phone 0901234567 in detail endpoint, got %v", dto.Phone)
	}
	if dto.LatestDocument == nil || dto.LatestDocument.DocNumber != "040123456060" {
		t.Fatalf("expected unmasked doc_number 040123456060 in detail endpoint, got %+v", dto.LatestDocument)
	}
	if dto.ReviewLock == nil || dto.ReviewLock.LockedByAccountID != 99 || dto.ReviewLock.IsLockedByOther {
		t.Fatalf("expected soft lock owned by admin 99, got %+v", dto.ReviewLock)
	}
	if len(auditSvc.logs) != 1 || auditSvc.logs[0].Action != "VIEW_PII" {
		t.Fatalf("expected mandatory VIEW_PII audit log entry, got %+v", auditSvc.logs)
	}
}

// 2. Approval Gatekeeping, Duplicate Blocker & Direct Re-Approve Prohibition
func TestAdminApproveDocument_BlocksBlindListViewApproval(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	// Attempting to approve without calling AdminGetCandidate first MUST fail
	err = svc.AdminApproveDocument(context.Background(), 99, 10)
	if !errors.Is(err, candidate.ErrApprovalRequiresDetailInspection) {
		t.Fatalf("expected ErrApprovalRequiresDetailInspection, got %v", err)
	}
}

func TestAdminApproveDocument_BlocksDuplicateID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	// Step 1: Admin opens detail view (satisfies gatekeeping)
	mockAdminGetCandidateQueries(mock, 10)
	if _, err := svc.AdminGetCandidate(context.Background(), 10, 99); err != nil {
		t.Fatalf("AdminGetCandidate() error = %v", err)
	}

	// Step 2: Admin attempts to approve, but duplicate ID check finds a collision
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT candidate_document_id.+FROM candidate_documents.+FOR UPDATE`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_type", "candidate_document_number",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url", "candidate_document_verification_status",
		}).AddRow(
			int64(500), "cccd", "040123456060",
			"https://cdn.example.com/front.jpg", "https://cdn.example.com/back.jpg",
			"https://cdn.example.com/portrait.jpg", "pending",
		))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("040123456060", int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	err = svc.AdminApproveDocument(context.Background(), 99, 10)
	if !errors.Is(err, candidate.ErrDuplicateDocumentBlocker) {
		t.Fatalf("expected ErrDuplicateDocumentBlocker, got %v", err)
	}
}

func TestAdminApproveDocument_ForbidsDirectReApproveOnRejectedProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	mockAdminGetCandidateQueries(mock, 10)
	if _, err := svc.AdminGetCandidate(context.Background(), 10, 99); err != nil {
		t.Fatalf("AdminGetCandidate() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT candidate_document_id.+FROM candidate_documents.+FOR UPDATE`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_type", "candidate_document_number",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url", "candidate_document_verification_status",
		}).AddRow(
			int64(500), "cccd", "040123456060",
			"https://cdn.example.com/front.jpg", "https://cdn.example.com/back.jpg",
			"https://cdn.example.com/portrait.jpg", "rejected",
		))
	mock.ExpectRollback()

	err = svc.AdminApproveDocument(context.Background(), 99, 10)
	if !errors.Is(err, candidate.ErrRejectToApproveForbidden) {
		t.Fatalf("expected ErrRejectToApproveForbidden, got %v", err)
	}
}

// 3. Soft Lock Concurrency Protection Between Two Admins
func TestSoftLock_BlocksSecondAdminMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	// Admin 1 (ID=101) opens candidate 10 -> acquires 5-min soft lock
	mockAdminGetCandidateQueries(mock, 10)
	dto1, err := svc.AdminGetCandidate(context.Background(), 10, 101)
	if err != nil {
		t.Fatalf("Admin 1 get error = %v", err)
	}
	if dto1.ReviewLock == nil || dto1.ReviewLock.IsLockedByOther {
		t.Fatalf("expected Admin 1 to hold lock, got %+v", dto1.ReviewLock)
	}

	// Admin 2 (ID=202) opens candidate 10 -> sees IsLockedByOther = true
	mockAdminGetCandidateQueries(mock, 10)
	dto2, err := svc.AdminGetCandidate(context.Background(), 10, 202)
	if err != nil {
		t.Fatalf("Admin 2 get error = %v", err)
	}
	if dto2.ReviewLock == nil || !dto2.ReviewLock.IsLockedByOther || dto2.ReviewLock.LockedByAccountID != 101 {
		t.Fatalf("expected Admin 2 to see lock owned by Admin 1 (101), got %+v", dto2.ReviewLock)
	}

	// Admin 2 attempts to approve or reject -> blocked by soft lock (409 Conflict)
	if err := svc.AdminApproveDocument(context.Background(), 202, 10); !errors.Is(err, candidate.ErrProfileLockedByAnotherAdmin) {
		t.Fatalf("expected ErrProfileLockedByAnotherAdmin on approve, got %v", err)
	}
	if err := svc.AdminRejectDocument(context.Background(), 202, 10, request.AdminRejectDocumentRequestDTO{Reason: "Ảnh mờ"}); !errors.Is(err, candidate.ErrProfileLockedByAnotherAdmin) {
		t.Fatalf("expected ErrProfileLockedByAnotherAdmin on reject, got %v", err)
	}
}

// 4. Revoke Approval Cascading Logic (Unpaid Auto-Cancel + Paid Block/Refund)
func TestAdminRevokeApproval_CascadesUnpaidAndBlocksOrRefundsPaid(t *testing.T) {
	t.Run("blocks when confirmed registration exists and paid_resolution is block", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer db.Close()

		svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE candidate_documents SET candidate_document_verification_status = 'rejected'`).
			WithArgs(int64(10), "Phát hiện ảnh CCCD chỉnh sửa").
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(`SELECT er\.exam_registration_id, er\.exam_registration_exam_seat_id, er\.exam_registration_status FROM exam_registrations er`).
			WithArgs(int64(10)).
			WillReturnRows(sqlmock.NewRows([]string{"exam_registration_id", "exam_registration_exam_seat_id", "exam_registration_status"}).
				AddRow(int64(77), int64(88), "confirmed"))
		mock.ExpectRollback()

		err = svc.AdminRevokeApproval(context.Background(), 99, 10, request.AdminRevokeApprovalRequestDTO{
			Reason:         "Phát hiện ảnh CCCD chỉnh sửa",
			PaidResolution: "block",
		})
		if !errors.Is(err, candidate.ErrRevokeBlockedByPaidRegistration) {
			t.Fatalf("expected ErrRevokeBlockedByPaidRegistration, got %v", err)
		}
	})

	t.Run("cancels unpaid held seat and triggers pending_refund on paid registration when paid_resolution is refund", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer db.Close()

		auditSvc := &recordingAuditService{}
		svc := candidate.NewService(db, auditSvc, nil, nil)

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE candidate_documents SET candidate_document_verification_status = 'rejected'`).
			WithArgs(int64(10), "Thu hồi để hoàn tiền").
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(`SELECT er\.exam_registration_id, er\.exam_registration_exam_seat_id, er\.exam_registration_status FROM exam_registrations er`).
			WithArgs(int64(10)).
			WillReturnRows(sqlmock.NewRows([]string{"exam_registration_id", "exam_registration_exam_seat_id", "exam_registration_status"}).
				AddRow(int64(71), int64(81), "pending_payment").
				AddRow(int64(72), int64(82), "confirmed"))

		// Registration 71 (pending_payment) -> cancelled + seat 81 released
		mock.ExpectExec(`UPDATE exam_registrations SET exam_registration_status = 'cancelled'`).
			WithArgs(int64(71)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE payments SET payment_status = 'cancelled'`).
			WithArgs(int64(71), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE exam_seats SET exam_seat_status = 'available'`).
			WithArgs(int64(81)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Registration 72 (confirmed) -> cancelled + payment pending_refund + seat 82 released
		mock.ExpectExec(`UPDATE exam_registrations SET exam_registration_status = 'cancelled'`).
			WithArgs(int64(72)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE payments SET payment_status = 'pending_refund'`).
			WithArgs(int64(72), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE exam_seats SET exam_seat_status = 'available'`).
			WithArgs(int64(82)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectCommit()

		err = svc.AdminRevokeApproval(context.Background(), 99, 10, request.AdminRevokeApprovalRequestDTO{
			Reason:         "Thu hồi để hoàn tiền",
			PaidResolution: "refund",
		})
		if err != nil {
			t.Fatalf("AdminRevokeApproval() unexpected error = %v", err)
		}
		if len(auditSvc.logs) != 1 || auditSvc.logs[0].Action != "REVOKE_APPROVAL" {
			t.Fatalf("expected REVOKE_APPROVAL audit log, got %+v", auditSvc.logs)
		}
	})
}

// 5. Re-open Review & Merge Integration Tests
func TestAdminReopenReview_And_MergeCandidates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	auditSvc := &recordingAuditService{}
	svc := candidate.NewService(db, auditSvc, nil, nil)

	// Re-open review: transitions rejected -> pending and writes REOPEN_REVIEW audit log
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE candidate_documents SET candidate_document_verification_status = 'pending'`).
		WithArgs(int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.AdminReopenReview(context.Background(), 99, 10, request.AdminReopenReviewRequestDTO{
		Reason: "Thí sinh đã bổ sung bản giải trình hợp lệ",
	}); err != nil {
		t.Fatalf("AdminReopenReview() error = %v", err)
	}
	if len(auditSvc.logs) != 1 || auditSvc.logs[0].Action != "REOPEN_REVIEW" {
		t.Fatalf("expected REOPEN_REVIEW audit log, got %+v", auditSvc.logs)
	}

	// Merge candidates via fn_merge_candidate_profiles
	summaryJSON, _ := json.Marshal(map[string]any{
		"primary_candidate_id":          10,
		"duplicate_candidate_id":        25,
		"duplicate_account_id":          250,
		"moved_registrations_count":     1,
		"cancelled_registrations_count": 1,
		"reason":                        "Gộp hồ sơ trùng CCCD",
	})
	mock.ExpectQuery(`SELECT fn_merge_candidate_profiles\(\$1, \$2, \$3, \$4\)`).
		WithArgs(int64(10), int64(25), int64(99), "Gộp hồ sơ trùng CCCD").
		WillReturnRows(sqlmock.NewRows([]string{"fn_merge_candidate_profiles"}).AddRow(summaryJSON))

	res, err := svc.AdminMergeCandidates(context.Background(), 99, request.AdminMergeCandidatesRequestDTO{
		PrimaryCandidateID:   10,
		DuplicateCandidateID: 25,
		Reason:               "Gộp hồ sơ trùng CCCD",
	})
	if err != nil {
		t.Fatalf("AdminMergeCandidates() error = %v", err)
	}
	if res.PrimaryCandidateID != 10 || res.DuplicateCandidateID != 25 || res.MovedRegistrationsCount != 1 {
		t.Fatalf("unexpected merge response: %+v", res)
	}
	// Verify duplicate account JWT tokens were immediately revoked
	if !middleware.IsAccountTokenRevoked(context.Background(), 250) {
		t.Fatalf("expected duplicate account 250 tokens to be revoked after merge")
	}
	middleware.RestoreAccountTokens(context.Background(), 250)
}

func TestHandler_OptimisticLockConflictReturns409(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)
	h := candidate.NewHandler(svc)
	_, _, r := testutils.SetupTestRouter()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(99))
	})
	r.POST("/admin/candidates/:id/reject", h.AdminRejectDocument)

	// Simulate 0 rows affected (another admin already processed the document)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE candidate_documents SET candidate_document_verification_status = 'rejected'`).
		WithArgs(int64(10), "Ảnh bị lóa").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req := testutils.BuildJSONRequest(http.MethodPost, "/admin/candidates/10/reject", `{"reason":"Ảnh bị lóa"}`)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on optimistic lock 0 rows affected, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandler_AdminGetCandidate_ZeroTrustPIIPinGateway(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	const jwtSecret = "zero-trust-secret"
	const adminID int64 = 99
	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)
	h := candidate.NewHandler(svc)
	h.SetJWTSecret(jwtSecret)

	_, _, r := testutils.SetupTestRouter()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", adminID)
		c.Set("user_role", "admin")
	})
	r.GET("/admin/candidates/:id", h.AdminGetCandidate)

	// 1. Call WITHOUT X-PII-Access-Token -> Raw image URLs must be withheld and pii_locked=true
	mockAdminGetCandidateQueries(mock, 10)
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/admin/candidates/10", nil)
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w1.Code)
	}
	var env1 struct {
		Data struct {
			LatestDocument struct {
				FrontImageURL    string  `json:"front_image_url"`
				BackImageURL     *string `json:"back_image_url"`
				PortraitImageURL string  `json:"portrait_image_url"`
				HasFrontImage    bool    `json:"has_front_image"`
				HasBackImage     bool    `json:"has_back_image"`
				HasPortraitImage bool    `json:"has_portrait_image"`
				PiiLocked        bool    `json:"pii_locked"`
			} `json:"latest_document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &env1); err != nil {
		t.Fatalf("unmarshal response 1 failed: %v", err)
	}
	if !env1.Data.LatestDocument.PiiLocked {
		t.Fatalf("expected pii_locked=true when X-PII-Access-Token is absent")
	}
	if env1.Data.LatestDocument.FrontImageURL != "" || env1.Data.LatestDocument.BackImageURL != nil || env1.Data.LatestDocument.PortraitImageURL != "" {
		t.Fatalf("expected raw image URLs to be withheld without pii_access_token, got %+v", env1.Data.LatestDocument)
	}
	if !env1.Data.LatestDocument.HasFrontImage || !env1.Data.LatestDocument.HasBackImage || !env1.Data.LatestDocument.HasPortraitImage {
		t.Fatalf("expected has_*_image boolean flags to remain true so UI knows photos exist")
	}

	// 2. Call WITH valid 5-minute X-PII-Access-Token (scope="pii_access") -> Raw image URLs unlocked
	piiToken, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":   adminID,
		"user_role": "admin",
		"scope":     middleware.ScopePIIAccess,
		"exp":       time.Now().Add(5 * time.Minute).Unix(),
	}).SignedString([]byte(jwtSecret))

	mockAdminGetCandidateQueries(mock, 10)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin/candidates/10", nil)
	req2.Header.Set(middleware.HeaderPIIAccessToken, piiToken)
	r.ServeHTTP(w2, req2)

	if err := json.Unmarshal(w2.Body.Bytes(), &env1); err != nil {
		t.Fatalf("unmarshal response 2 failed: %v", err)
	}
	if env1.Data.LatestDocument.PiiLocked {
		t.Fatalf("expected pii_locked=false when valid X-PII-Access-Token is supplied")
	}
	if env1.Data.LatestDocument.FrontImageURL != "https://cdn.example.com/front.jpg" ||
		env1.Data.LatestDocument.PortraitImageURL != "https://cdn.example.com/portrait.jpg" {
		t.Fatalf("expected unmasked image URLs after PIN unlock, got %+v", env1.Data.LatestDocument)
	}
}

func TestHandler_AdminGetCandidateAuditLogs_PaginatedAndHandlesNullOldValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)
	h := candidate.NewHandler(svc)

	_, _, r := testutils.SetupTestRouter()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(99))
		c.Set("user_role", "admin")
	})
	r.GET("/admin/candidates/:id/audit-logs", h.AdminGetCandidateAuditLogs)

	now := time.Date(2026, 11, 5, 20, 10, 0, 0, time.UTC)
	actorID := int64(99)

	// Page 1, limit 5, total 12 items -> total_pages = 3
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM audit_logs WHERE`).
		WithArgs(int64(49)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(12)))

	// Row 1 has NULL old_value (e.g., CREATE/REJECT) and Row 2 has both old_value and new_value (e.g., REOPEN_REVIEW)
	mock.ExpectQuery(`SELECT audit_log_id, audit_log_actor_account_id, audit_log_action`).
		WithArgs(int64(49), 5, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"audit_log_id", "audit_log_actor_account_id", "audit_log_action",
			"audit_log_entity_table", "audit_log_entity_id",
			"audit_log_old_value", "audit_log_new_value", "audit_log_created_at",
		}).
			AddRow(int64(102), &actorID, "REOPEN_REVIEW", "candidate_documents", int64(49), []byte(`{"verification_status":"rejected"}`), []byte(`{"verification_status":"pending","reason":"Thí sinh bổ sung ảnh rõ nét"}`), now).
			AddRow(int64(101), &actorID, "REJECT", "candidate_documents", int64(49), nil, []byte(`{"reason":"Ảnh CCCD bị mờ"}`), now.Add(-5*time.Minute)))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/candidates/49/audit-logs?scope=candidate_documents&page=1&limit=5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from GET /admin/candidates/:id/audit-logs, got %d; body: %s", w.Code, w.Body.String())
	}

	var env struct {
		Success bool `json:"success"`
		Data    []struct {
			ID          int64  `json:"id"`
			Action      string `json:"action"`
			EntityTable string `json:"entity_table"`
			EntityID    int64  `json:"entity_id"`
		} `json:"data"`
		Meta struct {
			Page       int   `json:"page"`
			PerPage    int   `json:"per_page"`
			TotalItems int64 `json:"total_items"`
			TotalPages int   `json:"total_pages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !env.Success || len(env.Data) != 2 {
		t.Fatalf("expected 2 audit log items, got %+v", env)
	}
	if env.Data[0].Action != "REOPEN_REVIEW" || env.Data[1].Action != "REJECT" {
		t.Fatalf("unexpected actions: %+v", env.Data)
	}
	if env.Meta.Page != 1 || env.Meta.PerPage != 5 || env.Meta.TotalItems != 12 || env.Meta.TotalPages != 3 {
		t.Fatalf("unexpected pagination meta: %+v", env.Meta)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestAdminReopenReview_EmitsProfileReopenedEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	notifPub := &recordingNotifPublisher{}
	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)
	svc.SetNotificationPublisher(notifPub)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE candidate_documents SET candidate_document_verification_status = 'pending'`).
		WithArgs(int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_account_id"}).AddRow(int64(50)))

	err = svc.AdminReopenReview(context.Background(), 99, 10, request.AdminReopenReviewRequestDTO{
		Reason: "Thí sinh đã liên hệ bổ sung giấy tờ hợp lệ",
	})
	if err != nil {
		t.Fatalf("AdminReopenReview() error = %v", err)
	}

	if len(notifPub.events) != 1 {
		t.Fatalf("expected 1 domain event published on AdminReopenReview, got %d", len(notifPub.events))
	}
	if notifPub.events[0].Type != notification.TypeProfileReopened || notifPub.events[0].UserID != 50 {
		t.Fatalf("unexpected domain event: %+v", notifPub.events[0])
	}
}


