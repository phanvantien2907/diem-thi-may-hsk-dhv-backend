package tests

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/exam"
	"github.com/hskvinh/backend/internal/exam/enum"
)

func TestExamSessionPublicationStatusValid(t *testing.T) {
	tests := []struct {
		name  string
		value enum.ExamSessionPublicationStatus
		valid bool
	}{
		{name: "published", value: enum.PublicationStatusPublished, valid: true},
		{name: "draft", value: enum.PublicationStatusDraft, valid: true},
		{name: "scheduled", value: enum.PublicationStatusScheduled, valid: true},
		{name: "unknown", value: "unknown", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.valid {
				t.Fatalf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestBatchCreateExamSessionsRejectsInvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := exam.NewHandler(nil)

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "missing sessions", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := gin.New()
			r.POST("/admin/exam-sessions/batch-create", handler.BatchCreateExamSessions)
			req := httptest.NewRequest(http.MethodPost, "/admin/exam-sessions/batch-create", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
		})
	}
}

func TestToExamSessionResponseDTODoesNotExposeExamRoom(t *testing.T) {
	publishAt := time.Date(2026, 10, 1, 8, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	checkInAt := publishAt.Add(30 * time.Minute)
	got := exam.ToExamSessionResponseDTO(exam.ExamSessionEntity{
		ExamSessionID:                   7,
		ExamSessionExamTypeID:           2,
		ExamSessionDate:                 time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC),
		ExamSessionShift:                enum.ShiftMorning,
		ExamSessionCapacity:             40,
		ExamSessionFee:                  500000,
		ExamSessionRegistrationDeadline: publishAt,
		ExamSessionStatus:               enum.SessionStatusOpen,
		ExamSessionPublicationStatus:    enum.PublicationStatusScheduled,
		ExamSessionPublishAt:            &publishAt,
		ExamSessionCheckInAt:            &checkInAt,
	})

	if got.ID != 7 || got.ExamTypeID != 2 {
		t.Fatalf("unexpected identity fields: %+v", got)
	}
	if got.PublicationStatus != "scheduled" || got.PublishAt == nil || got.CheckInAt == nil {
		t.Fatalf("publication/check-in fields were not mapped: %+v", got)
	}
}

func TestListExamSessionsReturnsPublicationAndCheckInFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	publishAt := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	checkInAt := publishAt.Add(30 * time.Minute)
	rows := sqlmock.NewRows([]string{
		"exam_session_id", "exam_session_exam_type_id", "exam_session_date",
		"exam_session_shift", "exam_session_capacity", "exam_session_fee",
		"exam_session_registration_deadline", "exam_session_status",
		"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
	}).AddRow(7, 2, time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), "morning", 40, 500000,
		publishAt, "open", "published", publishAt, checkInAt)
	mock.ExpectQuery(`(?s)SELECT exam_session_id, exam_session_exam_type_id,.*FROM exam_sessions`).
		WillReturnRows(rows)

	svc := exam.NewService(db, fakeAuditService{})
	got, err := svc.ListExamSessions(context.Background())
	if err != nil {
		t.Fatalf("ListExamSessions() error = %v", err)
	}
	if len(got) != 1 || got[0].PublicationStatus != "published" || got[0].CheckInAt == nil {
		t.Fatalf("unexpected sessions: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

type fakeAuditService struct{}

func (fakeAuditService) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return nil
}

func (fakeAuditService) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}
