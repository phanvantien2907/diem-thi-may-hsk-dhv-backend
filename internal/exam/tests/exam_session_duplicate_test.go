package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/exam"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	examres "github.com/hskvinh/backend/internal/exam/dto/response"
)

func TestService_AdminDuplicateExamSession_ValidDuplication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sourceSessionID := int64(10)
	parentPeriodID := int64(1)
	roomID := int64(5)
	actorID := int64(99)
	srcName := "HSK 3 - Ca sáng"
	srcDesc := "Phòng thi tiêu chuẩn"
	now := time.Now()

	mock.ExpectBegin()

	// 1. Lock source session
	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
		WithArgs(sourceSessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
			"exam_session_shift", "exam_session_fee",
		}).AddRow(
			sourceSessionID, &parentPeriodID, int64(3),
			&roomID, &srcName, &srcDesc,
			"morning", int64(1200000),
		))

	// 2. Check parent period status (active draft)
	mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
		WithArgs(parentPeriodID).
		WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow("draft"))

	// 3. Insert duplicated session
	expectedName := "HSK 3 - Ca sáng (Copy)"
	newSessionID := int64(25)
	mock.ExpectQuery(`INSERT INTO exam_sessions`).
		WithArgs(
			&parentPeriodID, int64(3), &roomID, expectedName, &srcDesc,
			"morning", int64(1200000), sourceSessionID,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_name", "exam_session_description",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
			"exam_session_duplicated_from_id", "exam_session_created_at", "exam_session_updated_at",
		}).AddRow(
			newSessionID, &parentPeriodID, int64(3),
			&expectedName, &srcDesc,
			nil, "morning", nil, int64(1200000),
			nil, "open",
			"draft", nil, nil,
			&sourceSessionID, now, now,
		))

	mock.ExpectCommit()

	res, err := svc.AdminDuplicateExamSession(context.Background(), sourceSessionID, actorID, examreq.DuplicateExamSessionRequestDTO{})
	if err != nil {
		t.Fatalf("AdminDuplicateExamSession() error = %v", err)
	}

	if res.ID != newSessionID {
		t.Fatalf("expected ID %d, got %d", newSessionID, res.ID)
	}
	if res.Name == nil || *res.Name != expectedName {
		t.Fatalf("expected Name %q, got %v", expectedName, res.Name)
	}
	if res.Date != nil {
		t.Fatalf("expected Date to be nil, got %v", res.Date)
	}
	if res.Capacity != nil {
		t.Fatalf("expected Capacity to be nil, got %v", res.Capacity)
	}
	if res.RegistrationDeadline != nil {
		t.Fatalf("expected RegistrationDeadline to be nil, got %v", res.RegistrationDeadline)
	}
	if res.PublishAt != nil || res.CheckInAt != nil {
		t.Fatalf("expected PublishAt/CheckInAt to be nil, got %+v", res)
	}
	if res.PublicationStatus != "draft" {
		t.Fatalf("expected draft status, got %s", res.PublicationStatus)
	}
	if res.DuplicatedFromID == nil || *res.DuplicatedFromID != sourceSessionID {
		t.Fatalf("expected DuplicatedFromID %d, got %v", sourceSessionID, res.DuplicatedFromID)
	}
	if res.Fee != 1200000 {
		t.Fatalf("expected Fee 1200000, got %d", res.Fee)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminDuplicateExamSession_ChainedDuplication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sourceSessionID := int64(10)
	parentPeriodID := int64(1)
	actorID := int64(99)
	srcName := "HSK 4 Ca chiều (Copy)"
	now := time.Now()

	mock.ExpectBegin()

	// 1. Lock source session which already has (Copy)
	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
		WithArgs(sourceSessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
			"exam_session_shift", "exam_session_fee",
		}).AddRow(
			sourceSessionID, &parentPeriodID, int64(4),
			nil, &srcName, nil,
			"afternoon", int64(1500000),
		))

	// 2. Parent period status
	mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
		WithArgs(parentPeriodID).
		WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow("registration_open"))

	// 3. Insert duplicated session: Notice expectedName retains single " (Copy)"
	expectedName := "HSK 4 Ca chiều (Copy)"
	mock.ExpectQuery(`INSERT INTO exam_sessions`).
		WithArgs(
			&parentPeriodID, int64(4), nil, expectedName, nil,
			"afternoon", int64(1500000), sourceSessionID,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_name", "exam_session_description",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
			"exam_session_duplicated_from_id", "exam_session_created_at", "exam_session_updated_at",
		}).AddRow(
			int64(26), &parentPeriodID, int64(4),
			&expectedName, nil,
			nil, "afternoon", nil, int64(1500000),
			nil, "open",
			"draft", nil, nil,
			&sourceSessionID, now, now,
		))

	mock.ExpectCommit()

	res, err := svc.AdminDuplicateExamSession(context.Background(), sourceSessionID, actorID, examreq.DuplicateExamSessionRequestDTO{})
	if err != nil {
		t.Fatalf("AdminDuplicateExamSession() error = %v", err)
	}

	if res.Name == nil || *res.Name != expectedName {
		t.Fatalf("expected Name %q, got %v", expectedName, res.Name)
	}
	if strings.Contains(*res.Name, "(Copy) (Copy)") {
		t.Fatalf("chained copy created duplicated suffix: %s", *res.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminDuplicateExamSession_CustomNameOverride(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sourceSessionID := int64(10)
	parentPeriodID := int64(1)
	actorID := int64(99)
	customName := "Ca thi HSK 5 Đặc Biệt"
	now := time.Now()

	mock.ExpectBegin()

	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
		WithArgs(sourceSessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
			"exam_session_shift", "exam_session_fee",
		}).AddRow(
			sourceSessionID, &parentPeriodID, int64(5),
			nil, nil, nil,
			"evening", int64(1800000),
		))

	mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
		WithArgs(parentPeriodID).
		WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow("draft"))

	mock.ExpectQuery(`INSERT INTO exam_sessions`).
		WithArgs(
			&parentPeriodID, int64(5), nil, customName, nil,
			"evening", int64(1800000), sourceSessionID,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
			"exam_session_name", "exam_session_description",
			"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
			"exam_session_registration_deadline", "exam_session_status",
			"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
			"exam_session_duplicated_from_id", "exam_session_created_at", "exam_session_updated_at",
		}).AddRow(
			int64(27), &parentPeriodID, int64(5),
			&customName, nil,
			nil, "evening", nil, int64(1800000),
			nil, "open",
			"draft", nil, nil,
			&sourceSessionID, now, now,
		))

	mock.ExpectCommit()

	res, err := svc.AdminDuplicateExamSession(context.Background(), sourceSessionID, actorID, examreq.DuplicateExamSessionRequestDTO{
		Name: &customName,
	})
	if err != nil {
		t.Fatalf("AdminDuplicateExamSession() error = %v", err)
	}

	if res.Name == nil || *res.Name != customName {
		t.Fatalf("expected custom Name %q, got %v", customName, res.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_AdminDuplicateExamSession_ParentClosedPeriodBlocked(t *testing.T) {
	testCases := []struct {
		name         string
		periodStatus string
	}{
		{name: "registration_closed", periodStatus: "registration_closed"},
		{name: "cancelled", periodStatus: "cancelled"},
		{name: "completed", periodStatus: "completed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			svc := exam.NewService(db, fakeAuditService{})
			sourceSessionID := int64(10)
			parentPeriodID := int64(1)
			actorID := int64(99)

			mock.ExpectBegin()

			mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
				WithArgs(sourceSessionID).
				WillReturnRows(sqlmock.NewRows([]string{
					"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
					"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
					"exam_session_shift", "exam_session_fee",
				}).AddRow(
					sourceSessionID, &parentPeriodID, int64(3),
					nil, nil, nil,
					"morning", int64(1200000),
				))

			mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
				WithArgs(parentPeriodID).
				WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow(tc.periodStatus))

			mock.ExpectRollback()

			_, err = svc.AdminDuplicateExamSession(context.Background(), sourceSessionID, actorID, examreq.DuplicateExamSessionRequestDTO{})
			if !errors.Is(err, exam.ErrParentPeriodClosed) {
				t.Fatalf("expected ErrParentPeriodClosed, got %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestService_AdminDuplicateExamSession_NonExistentOrDeleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := exam.NewService(db, fakeAuditService{})
	sourceSessionID := int64(9999)
	actorID := int64(99)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
		WithArgs(sourceSessionID).
		WillReturnError(sqlmock.ErrCancelled) // or any no rows error
	mock.ExpectRollback()

	_, err = svc.AdminDuplicateExamSession(context.Background(), sourceSessionID, actorID, examreq.DuplicateExamSessionRequestDTO{})
	if err == nil {
		t.Fatalf("expected error for non-existent session, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestService_UpdatePublicationStatus_BlocksMissingSlotsOrDate(t *testing.T) {
	testCases := []struct {
		name     string
		sessDate *time.Time
		capacity *int
		deadline *time.Time
	}{
		{
			name:     "all_nil",
			sessDate: nil,
			capacity: nil,
			deadline: nil,
		},
		{
			name:     "date_nil_capacity_set",
			sessDate: nil,
			capacity: func() *int { c := 30; return &c }(),
			deadline: func() *time.Time { t := time.Now(); return &t }(),
		},
		{
			name:     "date_set_capacity_nil",
			sessDate: func() *time.Time { t := time.Now(); return &t }(),
			capacity: nil,
			deadline: func() *time.Time { t := time.Now(); return &t }(),
		},
		{
			name:     "capacity_zero",
			sessDate: func() *time.Time { t := time.Now(); return &t }(),
			capacity: func() *int { c := 0; return &c }(),
			deadline: func() *time.Time { t := time.Now(); return &t }(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			svc := exam.NewService(db, fakeAuditService{})
			sessionID := int64(30)
			actorID := int64(99)

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT exam_session_publication_status, exam_session_status`).
				WithArgs(sessionID).
				WillReturnRows(sqlmock.NewRows([]string{
					"exam_session_publication_status", "exam_session_status",
					"exam_session_date", "exam_session_capacity", "exam_session_registration_deadline",
				}).AddRow("draft", "open", tc.sessDate, tc.capacity, tc.deadline))

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
		})
	}
}

func TestHandler_AdminDuplicateExamSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("success_201", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer db.Close()

		svc := exam.NewService(db, fakeAuditService{})
		h := exam.NewHandler(svc)
		sourceSessionID := int64(10)
		parentPeriodID := int64(1)
		now := time.Now()
		expectedName := "HSK 2 - Sáng (Copy)"

		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
			WithArgs(sourceSessionID).
			WillReturnRows(sqlmock.NewRows([]string{
				"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
				"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
				"exam_session_shift", "exam_session_fee",
			}).AddRow(
				sourceSessionID, &parentPeriodID, int64(2),
				nil, "HSK 2 - Sáng", nil,
				"morning", int64(900000),
			))

		mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
			WithArgs(parentPeriodID).
			WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow("draft"))

		mock.ExpectQuery(`INSERT INTO exam_sessions`).
			WillReturnRows(sqlmock.NewRows([]string{
				"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
				"exam_session_name", "exam_session_description",
				"exam_session_date", "exam_session_shift", "exam_session_capacity", "exam_session_fee",
				"exam_session_registration_deadline", "exam_session_status",
				"exam_session_publication_status", "exam_session_publish_at", "exam_session_check_in_at",
				"exam_session_duplicated_from_id", "exam_session_created_at", "exam_session_updated_at",
			}).AddRow(
				int64(99), &parentPeriodID, int64(2),
				&expectedName, nil,
				nil, "morning", nil, int64(900000),
				nil, "open",
				"draft", nil, nil,
				&sourceSessionID, now, now,
			))

		mock.ExpectCommit()

		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", int64(99))
			c.Next()
		})
		r.POST("/admin/exam-sessions/:id/duplicate", h.AdminDuplicateExamSession)

		req := httptest.NewRequest(http.MethodPost, "/admin/exam-sessions/10/duplicate", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool                          `json:"success"`
			Data    examres.ExamSessionResponseDTO `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Data.ID != 99 || resp.Data.PublicationStatus != "draft" {
			t.Fatalf("unexpected response payload: %+v", resp.Data)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
	})

	t.Run("parent_closed_period_409", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer db.Close()

		svc := exam.NewService(db, fakeAuditService{})
		h := exam.NewHandler(svc)
		sourceSessionID := int64(10)
		parentPeriodID := int64(1)

		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT exam_session_id, exam_session_exam_period_id.*FROM exam_sessions.*FOR SHARE`).
			WithArgs(sourceSessionID).
			WillReturnRows(sqlmock.NewRows([]string{
				"exam_session_id", "exam_session_exam_period_id", "exam_session_exam_type_id",
				"exam_session_exam_room_id", "exam_session_name", "exam_session_description",
				"exam_session_shift", "exam_session_fee",
			}).AddRow(
				sourceSessionID, &parentPeriodID, int64(2),
				nil, "HSK 2 - Sáng", nil,
				"morning", int64(900000),
			))

		mock.ExpectQuery(`SELECT exam_period_status FROM exam_periods WHERE exam_period_id = \$1`).
			WithArgs(parentPeriodID).
			WillReturnRows(sqlmock.NewRows([]string{"exam_period_status"}).AddRow("completed"))

		mock.ExpectRollback()

		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", int64(99))
			c.Next()
		})
		r.POST("/admin/exam-sessions/:id/duplicate", h.AdminDuplicateExamSession)

		req := httptest.NewRequest(http.MethodPost, "/admin/exam-sessions/10/duplicate", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d, body: %s", w.Code, w.Body.String())
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
	})
}
