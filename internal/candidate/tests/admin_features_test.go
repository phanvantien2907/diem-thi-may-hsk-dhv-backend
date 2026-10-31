package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
)

func TestService_AdminListCandidatesFiltered(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	req := request.AdminListCandidatesRequestDTO{
		Status: "verified",
		Level:  "HSK 3",
		Search: "Nguyen",
		Page:   2,
		Limit:  1,
	}

	mock.ExpectQuery(`(?s)WITH latest_documents AS.*SELECT COUNT\(\*\).*FROM candidates c.*COALESCE\(ld\.status, 'pending'\) = \$1.*et\.exam_type_name ILIKE \$2.*c\.candidate_full_name ILIKE \$3`).
		WithArgs("verified", "%HSK 3%", "%Nguyen%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	dob := time.Date(2000, 5, 15, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WITH latest_documents AS.*SELECT c\.candidate_id.*ORDER BY c\.candidate_id DESC.*LIMIT \$4 OFFSET \$5`).
		WithArgs("verified", "%HSK 3%", "%Nguyen%", 1, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_full_name_cn",
			"candidate_dob", "candidate_gender", "candidate_ethnicity", "candidate_religion",
			"candidate_birthplace", "candidate_nationality", "candidate_mother_tongue",
			"candidate_chinese_study_years", "account_phone", "account_email", "exam_type_name",
			"candidate_document_id", "candidate_id", "candidate_document_type", "candidate_document_number",
			"candidate_document_issue_date", "candidate_document_issue_place",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url", "status", "candidate_document_rejection_reason",
			"candidate_document_ward_id", "province_id", "candidate_document_address_detail",
		}).AddRow(
			int64(10), int64(20), "潘文进", "潘文进", dob, "male", nil, nil, nil,
			"Việt Nam", nil, nil, "0901234567", "candidate@example.com", "HSK 3",
			int64(30), int64(10), "cccd", "012345678901", dob, "公安机关",
			"front", nil, "portrait", "verified", nil, nil, nil, nil,
		))

	items, meta, err := svc.AdminListCandidatesFiltered(context.Background(), req)
	if err != nil {
		t.Fatalf("AdminListCandidatesFiltered() error = %v", err)
	}
	if len(items) != 1 || items[0].FullName != "潘文进" ||
		items[0].FullNameCN == nil || *items[0].FullNameCN != "潘文进" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].Phone == nil || *items[0].Phone != "090***567" ||
		items[0].ExamLevel == nil || *items[0].ExamLevel != "HSK 3" ||
		items[0].TargetExamLevel == nil || *items[0].TargetExamLevel != "HSK 3" ||
		items[0].LatestDocument == nil || items[0].LatestDocument.DocNumber != "012***901" ||
		items[0].LatestDocument.BackImageURL != nil {
		t.Fatalf("unexpected enriched candidate fields: %+v", items[0])
	}
	payload, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, field := range []string{`"full_name_cn":"潘文进"`, `"birthplace":null`, `"back_image_url":null`} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("expected explicit null field %s in %s", field, payload)
		}
	}
	if meta.Page != 2 || meta.PerPage != 1 || meta.TotalItems != 2 || meta.TotalPages != 2 {
		t.Fatalf("unexpected pagination meta: %+v", meta)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminCandidateMetrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	mock.ExpectQuery(`(?s)WITH latest_documents AS.*COUNT\(\*\) AS total.*WHERE c\.candidate_is_deleted = FALSE`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "pending", "verified", "rejected"}).
			AddRow(int64(10), int64(4), int64(5), int64(1)))

	metrics, err := svc.AdminCandidateMetrics(context.Background())
	if err != nil {
		t.Fatalf("AdminCandidateMetrics() error = %v", err)
	}
	if metrics.Total != 10 || metrics.Pending != 4 || metrics.Verified != 5 || metrics.Rejected != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminExportCandidatesByExamDate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	examDate := "2026-11-27"
	registeredAt := time.Date(2026, 10, 6, 8, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	dob := time.Date(2000, 5, 15, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)WITH latest_documents AS.*batch_registrations AS.*es\.exam_session_date = \$1.*SELECT c\.candidate_full_name, c\.candidate_dob.*ORDER BY c\.candidate_full_name ASC`).
		WithArgs(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_full_name", "candidate_dob", "candidate_document_number",
			"exam_type_name", "exam_registration_registered_at", "status",
			"candidate_document_rejection_reason",
		}).AddRow("Nguyen Van A", dob, "012345678901", "HSK 3", registeredAt, "rejected", "Ảnh CCCD bị mờ"))

	pdf, err := svc.AdminExportCandidates(context.Background(), request.AdminListCandidatesRequestDTO{
		ExamDate: examDate,
	})
	if err != nil {
		t.Fatalf("AdminExportCandidates() error = %v", err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-") {
		t.Fatalf("export is not a PDF: %q", string(pdf[:minInt(len(pdf), 20)]))
	}
	pdfText := string(pdf)
	for _, want := range []string{"/FontFile2", "xref"} {
		if !strings.Contains(pdfText, want) {
			t.Errorf("PDF does not contain expected marker %q", want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminExportCandidates_AllStatusDoesNotFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	mock.ExpectQuery(`(?s)WITH latest_documents AS.*SELECT c\.candidate_full_name.*ORDER BY c\.candidate_full_name ASC`).
		WithArgs(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_full_name", "candidate_dob", "candidate_document_number",
			"exam_type_name", "exam_registration_registered_at", "status",
			"candidate_document_rejection_reason",
		}))

	if _, err := svc.AdminExportCandidates(context.Background(), request.AdminListCandidatesRequestDTO{
		ExamDate: "2026-11-27",
		Status:   "all",
	}); err != nil {
		t.Fatalf("AdminExportCandidates() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminExportCandidates_InvalidExamDate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, nil, nil, nil)
	_, err = svc.AdminExportCandidates(context.Background(), request.AdminListCandidatesRequestDTO{ExamDate: "27/11/2026"})
	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("expected invalid date error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database call: %v", err)
	}
}

func TestHandler_AdminExportCandidates_RequiresExamDate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	h := candidate.NewHandler(candidate.NewService(db, nil, nil, nil))
	r := gin.New()
	r.GET("/admin/candidates/export", h.AdminExportCandidates)

	req := httptest.NewRequest(http.MethodGet, "/admin/candidates/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
