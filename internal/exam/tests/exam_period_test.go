package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/exam"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	examres "github.com/hskvinh/backend/internal/exam/dto/response"
	"github.com/lib/pq"
)

func TestService_AdminDuplicateExamPeriod(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := fakeAuditService{}
	svc := exam.NewService(db, audit)
	sourcePeriodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()

	// 1. Lock source period
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name, exam_period_code.*FROM exam_periods.*FOR SHARE`).
		WithArgs(sourcePeriodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
		}).AddRow(sourcePeriodID, "Đợt thi Tháng 10/2026", "HSK-2026-10", "Mô tả đợt thi", "Hướng dẫn"))

	// 2. Insert cloned period in 'draft'
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO exam_periods`).
		WithArgs("Đợt thi Tháng 11/2026", "HSK-2026-11", "Mô tả đợt thi", "Hướng dẫn").
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			int64(2), "Đợt thi Tháng 11/2026", "HSK-2026-11",
			"Mô tả đợt thi", "Hướng dẫn", nil, nil, nil, nil, "draft", now, now,
		))

	// 3. Cloned child sessions via INSERT ... SELECT
	mock.ExpectExec(`INSERT INTO exam_sessions.*SELECT \$1.*FROM exam_sessions.*WHERE exam_session_exam_period_id = \$2`).
		WithArgs(int64(2), sourcePeriodID).
		WillReturnResult(sqlmock.NewResult(0, 2))

	mock.ExpectCommit()

	req := examreq.DuplicateExamPeriodRequestDTO{
		Name: "Đợt thi Tháng 11/2026",
		Code: "HSK-2026-11",
	}

	res, err := svc.AdminDuplicateExamPeriod(context.Background(), sourcePeriodID, actorID, req)
	if err != nil {
		t.Fatalf("AdminDuplicateExamPeriod() error = %v", err)
	}

	if res.NewPeriodID != 2 || res.DuplicatedSessions != 2 {
		t.Fatalf("unexpected duplicate result: %+v", res)
	}
	if res.Period.Status != "draft" || res.Period.Name != "Đợt thi Tháng 11/2026" {
		t.Fatalf("unexpected period response: %+v", res.Period)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminUpdateExamPeriod_NonDraftBlocksDateEdit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, nil)
	periodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()

	// Return period with status 'registration_open'
	now := time.Now()
	startDate := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name.*FROM exam_periods.*FOR UPDATE`).
		WithArgs(periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10",
			"Mô tả", "Hướng dẫn", &startDate, &startDate, nil, nil, "registration_open", now, now,
		))

	mock.ExpectRollback()

	newStartDate := "2026-10-25"
	req := examreq.UpdateExamPeriodRequestDTO{
		StartDate: &newStartDate,
	}

	_, err = svc.AdminUpdateExamPeriod(context.Background(), periodID, actorID, req)
	if err != exam.ErrNonDraftDateModification {
		t.Fatalf("expected ErrNonDraftDateModification, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminUpdateExamPeriod_NonDraftAllowsSafeFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := fakeAuditService{}
	svc := exam.NewService(db, audit)
	periodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()

	now := time.Now()
	startDate := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name.*FROM exam_periods.*FOR UPDATE`).
		WithArgs(periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10",
			"Mô tả cũ", "Hướng dẫn cũ", &startDate, &startDate, nil, nil, "published", now, now,
		))

	updatedDesc := "Mô tả cập nhật an toàn"
	mock.ExpectQuery(`UPDATE exam_periods SET exam_period_name = \$1.*WHERE exam_period_id = \$8`).
		WithArgs("Đợt thi Tháng 10/2026", &updatedDesc, "Hướng dẫn cũ", &startDate, &startDate, nil, nil, periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10",
			&updatedDesc, "Hướng dẫn cũ", &startDate, &startDate, nil, nil, "published", now, now,
		))

	mock.ExpectCommit()

	req := examreq.UpdateExamPeriodRequestDTO{
		Description: &updatedDesc,
	}

	res, err := svc.AdminUpdateExamPeriod(context.Background(), periodID, actorID, req)
	if err != nil {
		t.Fatalf("AdminUpdateExamPeriod() error = %v", err)
	}

	if res.Description == nil || *res.Description != updatedDesc {
		t.Fatalf("unexpected updated description: %+v", res)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminRescheduleExamPeriod(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := fakeAuditService{}
	svc := exam.NewService(db, audit)
	periodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()

	now := time.Now()
	startDate := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 9, 27, 23, 59, 59, 0, time.UTC)
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name, exam_period_code.*FROM exam_periods.*FOR UPDATE`).
		WithArgs(periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_deadline", "exam_period_status",
		}).AddRow(periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10", &startDate, &startDate, &deadline, "published"))

	newStartDate := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`UPDATE exam_periods SET exam_period_start_date = \$1.*exam_period_status = 'rescheduled'`).
		WithArgs(newStartDate, newStartDate, nil, periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10",
			nil, nil, &newStartDate, &newStartDate, nil, &deadline, "rescheduled", now, now,
		))

	// Cascade update to sub-sessions
	mock.ExpectExec(`UPDATE exam_sessions SET exam_session_date = COALESCE.*WHERE exam_session_exam_period_id = \$4`).
		WithArgs(34, newStartDate, nil, periodID).
		WillReturnResult(sqlmock.NewResult(0, 4))

	// Insert bulk notifications
	mock.ExpectExec(`INSERT INTO notifications`).
		WithArgs("Đợt thi Tháng 10/2026", "20/11/2026", "Bão lụt bất khả kháng", periodID).
		WillReturnResult(sqlmock.NewResult(0, 15))

	mock.ExpectCommit()

	req := examreq.RescheduleExamPeriodRequestDTO{
		NewStartDate: "2026-11-20",
		Reason:       "Bão lụt bất khả kháng",
	}

	res, err := svc.AdminRescheduleExamPeriod(context.Background(), periodID, actorID, req)
	if err != nil {
		t.Fatalf("AdminRescheduleExamPeriod() error = %v", err)
	}

	if res.UpdatedSessions != 4 || res.CandidatesAlerted != 15 || res.Reason != "Bão lụt bất khả kháng" {
		t.Fatalf("unexpected reschedule result: %+v", res)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminCloseRegistration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := fakeAuditService{}
	svc := exam.NewService(db, audit)
	periodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()

	now := time.Now()
	mock.ExpectQuery(`UPDATE exam_periods SET exam_period_status = 'registration_closed'.*WHERE exam_period_id = \$1`).
		WithArgs(periodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			periodID, "Đợt thi Tháng 10/2026", "HSK-2026-10",
			nil, nil, nil, nil, nil, nil, "registration_closed", now, now,
		))

	mock.ExpectExec(`UPDATE exam_sessions SET exam_session_status = 'closed'.*WHERE exam_session_exam_period_id = \$1`).
		WithArgs(periodID).
		WillReturnResult(sqlmock.NewResult(0, 4))

	mock.ExpectCommit()

	res, err := svc.AdminCloseRegistration(context.Background(), periodID, actorID, examreq.CloseRegistrationRequestDTO{Reason: "Đã đủ chỉ tiêu sớm"})
	if err != nil {
		t.Fatalf("AdminCloseRegistration() error = %v", err)
	}

	if res.Status != "registration_closed" {
		t.Fatalf("expected registration_closed, got %s", res.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandler_AdminDuplicateExamPeriod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	audit := fakeAuditService{}
	svc := exam.NewService(db, audit)
	h := exam.NewHandler(svc)

	sourcePeriodID := int64(1)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name, exam_period_code.*FROM exam_periods.*FOR SHARE`).
		WithArgs(sourcePeriodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
		}).AddRow(sourcePeriodID, "Đợt thi gốc", "HSK-SRC", nil, nil))

	mock.ExpectQuery(`INSERT INTO exam_periods`).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
			"exam_period_start_date", "exam_period_end_date",
			"exam_period_registration_start", "exam_period_registration_deadline",
			"exam_period_status", "exam_period_created_at", "exam_period_updated_at",
		}).AddRow(
			int64(2), "Đợt thi gốc (Bản sao)", "HSK-SRC-COPY-1",
			nil, nil, nil, nil, nil, nil, "draft", now, now,
		))

	mock.ExpectExec(`INSERT INTO exam_sessions.*SELECT \$1.*FROM exam_sessions.*WHERE exam_session_exam_period_id = \$2`).
		WithArgs(int64(2), sourcePeriodID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(99))
		c.Next()
	})
	r.POST("/admin/exam-periods/:id/duplicate", h.AdminDuplicateExamPeriod)

	req := httptest.NewRequest(http.MethodPost, "/admin/exam-periods/1/duplicate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool                                   `json:"success"`
		Data    examres.DuplicateExamPeriodResponseDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Data.NewPeriodID != 2 || resp.Data.Period.Status != "draft" {
		t.Fatalf("unexpected response payload: %+v", resp.Data)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminDuplicateExamPeriod_CodeCollision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sourcePeriodID := int64(1)
	actorID := int64(99)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT exam_period_id, exam_period_name, exam_period_code.*FROM exam_periods.*FOR SHARE`).
		WithArgs(sourcePeriodID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_name", "exam_period_code",
			"exam_period_description", "exam_period_instructions",
		}).AddRow(sourcePeriodID, "Đợt thi gốc", "HSK-SRC", nil, nil))

	mock.ExpectQuery(`INSERT INTO exam_periods`).
		WithArgs("Tên trùng", "HSK-EXISTS", nil, nil).
		WillReturnError(&pq.Error{Code: "23505", Message: "duplicate key value violates unique constraint"})

	mock.ExpectRollback()

	req := examreq.DuplicateExamPeriodRequestDTO{
		Name: "Tên trùng",
		Code: "HSK-EXISTS",
	}

	_, err = svc.AdminDuplicateExamPeriod(context.Background(), sourcePeriodID, actorID, req)
	if !errors.Is(err, exam.ErrExamPeriodCodeExists) {
		t.Fatalf("expected ErrExamPeriodCodeExists, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminUpdateExamSession_DraftSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sessionID := int64(10)
	actorID := int64(99)

	mock.ExpectBegin()

	// 1. Lock session (in draft state)
	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR UPDATE`).
		WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
		}).AddRow(
			sessionID, int64(1), int64(1),
			nil, "morning", 40, int64(1300000),
			nil, "open", "draft", nil, nil,
		))

	newDate := "2027-01-15"
	newFee := int64(1400000)
	parsedDate := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)

	// 2. Expect UPDATE
	mock.ExpectQuery(`UPDATE exam_sessions SET exam_session_date = \$1.*WHERE exam_session_id = \$7`).
		WithArgs(&parsedDate, "morning", 40, newFee, nil, nil, sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
		}).AddRow(
			sessionID, int64(1), int64(1),
			&parsedDate, "morning", 40, newFee,
			nil, "open", "draft", nil, nil,
		))

	mock.ExpectCommit()

	res, err := svc.AdminUpdateExamSession(context.Background(), sessionID, actorID, examreq.UpdateExamSessionRequestDTO{
		Date: &newDate,
		Fee:  &newFee,
	})
	if err != nil {
		t.Fatalf("AdminUpdateExamSession() error = %v", err)
	}

	if res.Fee != newFee || res.Date == nil || *res.Date != parsedDate {
		t.Fatalf("unexpected updated session: %+v", res)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminUpdateExamSession_NonDraftBlocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sessionID := int64(10)
	actorID := int64(99)

	mock.ExpectBegin()

	// 1. Lock session (in published state)
	sessDate := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR UPDATE`).
		WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
		}).AddRow(
			sessionID, int64(1), int64(1),
			&sessDate, "morning", 40, int64(1300000),
			&sessDate, "open", "published", nil, nil,
		))

	// 2. Query active registrations count
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM exam_registrations WHERE exam_registration_exam_session_id = \$1`).
		WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectRollback()

	newFee := int64(1500000)
	_, err = svc.AdminUpdateExamSession(context.Background(), sessionID, actorID, examreq.UpdateExamSessionRequestDTO{
		Fee: &newFee,
	})
	if !errors.Is(err, exam.ErrSessionNotEditable) {
		t.Fatalf("expected ErrSessionNotEditable, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_UpdatePublicationStatus_BlocksIncompleteSchedule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sessionID := int64(15)
	actorID := int64(99)

	mock.ExpectBegin()
	// Date and deadline are NULL (as when cloned)
	mock.ExpectQuery(`SELECT exam_session_publication_status, exam_session_status`).
		WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_publication_status", "exam_session_status",
			"exam_session_date", "exam_session_registration_deadline",
		}).AddRow("draft", "open", nil, nil))

	mock.ExpectRollback()

	_, err = svc.UpdatePublicationStatus(context.Background(), sessionID, actorID, examreq.UpdatePublicationStatusRequestDTO{
		PublicationStatus: "published",
	})
	if !errors.Is(err, exam.ErrSessionScheduleIncomplete) {
		t.Fatalf("expected ErrSessionScheduleIncomplete, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
