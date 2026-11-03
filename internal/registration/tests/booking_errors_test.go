package tests

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/registration"
	"github.com/hskvinh/backend/internal/registration/dto/request"
)

// The status guards live in fn_assert_session_accepts_registration and surface as
// plpgsql RAISE EXCEPTION names. They must be translated into Vietnamese messages
// rather than leaking raw database errors to the client.
func TestReserveSeat_MapsSessionStateExceptions(t *testing.T) {
	cases := []struct {
		name     string
		pgerr    string
		wantText string
	}{
		{name: "session not found", pgerr: "EXAM_SESSION_NOT_FOUND", wantText: "Không tìm thấy ca thi"},
		{name: "session cancelled", pgerr: "EXAM_SESSION_CANCELLED", wantText: "Ca thi đã bị hủy"},
		{name: "session completed", pgerr: "EXAM_SESSION_COMPLETED", wantText: "Ca thi đã kết thúc"},
		{name: "session not published", pgerr: "EXAM_SESSION_NOT_PUBLISHED", wantText: "Ca thi chưa được công khai"},
		{name: "period cancelled", pgerr: "EXAM_PERIOD_CANCELLED", wantText: "Đợt thi đã bị hủy"},
		{name: "registration closed", pgerr: "REGISTRATION_CLOSED", wantText: "Đã hết thời gian đăng ký của ca thi"},
		{name: "duplicate registration", pgerr: "DUPLICATE_REGISTRATION", wantText: "Bạn đã đăng ký ca thi này rồi"},
		{name: "no seat", pgerr: "NO_SEAT_AVAILABLE", wantText: "Đã hết ghế trống cho ca thi này"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			mock.ExpectQuery("SELECT fn_book_exam_seat").
				WithArgs(int64(7), int64(3)).
				WillReturnError(errors.New("pq: " + tc.pgerr))

			if tc.pgerr == "DUPLICATE_REGISTRATION" {
				mock.ExpectQuery("SELECT exam_registration_id").
					WithArgs(int64(7), int64(3)).
					WillReturnError(sql.ErrNoRows)
			}

			svc := registration.NewService(db, &fakeAudit{})
			_, err = svc.ReserveSeat(context.Background(), request.ReserveSeatRequestDTO{ExamSessionID: 3}, 7)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got := err.Error(); got != tc.wantText {
				t.Fatalf("error = %q, want %q", got, tc.wantText)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sqlmock expectations: %v", err)
			}
		})
	}
}

func TestReserveSeat_GracefulDuplicateResolution(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	// 1. fn_book_exam_seat fails with DUPLICATE_REGISTRATION (e.g. race condition/double-click)
	mock.ExpectQuery("SELECT fn_book_exam_seat").
		WithArgs(int64(7), int64(3)).
		WillReturnError(errors.New("pq: DUPLICATE_REGISTRATION"))

	regAt := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	// 2. Graceful fallback queries pending_payment registration and finds it
	mock.ExpectQuery("SELECT exam_registration_id.*FROM exam_registrations").
		WithArgs(int64(7), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_id", "exam_registration_candidate_id",
			"exam_registration_exam_session_id", "exam_registration_exam_seat_id",
			"exam_registration_status", "exam_registration_registered_at",
		}).AddRow(int64(99), int64(7), int64(3), int64(501), "pending_payment", regAt))

	svc := registration.NewService(db, &fakeAudit{})
	res, err := svc.ReserveSeat(context.Background(), request.ReserveSeatRequestDTO{ExamSessionID: 3}, 7)
	if err != nil {
		t.Fatalf("expected graceful success, got error: %v", err)
	}
	if res.ID != 99 || res.Status != "pending_payment" || res.SessionID != 3 || res.SeatID != 501 {
		t.Fatalf("unexpected graceful registration response: %+v", res)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestTransferRegistration_MapsTargetSessionStateExceptions(t *testing.T) {
	cases := []struct {
		name     string
		pgerr    string
		wantText string
	}{
		{name: "registration not found", pgerr: "REGISTRATION_NOT_FOUND", wantText: "Không tìm thấy phiếu đăng ký"},
		{name: "registration not transferable", pgerr: "REGISTRATION_NOT_TRANSFERABLE", wantText: "Phiếu đăng ký không thể chuyển ca"},
		{name: "same session", pgerr: "TRANSFER_SAME_SESSION", wantText: "Không thể chuyển sang cùng ca thi"},
		{name: "target session cancelled", pgerr: "EXAM_SESSION_CANCELLED", wantText: "Ca thi đã bị hủy"},
		{name: "target session not published", pgerr: "EXAM_SESSION_NOT_PUBLISHED", wantText: "Ca thi chưa được công khai"},
		{name: "target period cancelled", pgerr: "EXAM_PERIOD_CANCELLED", wantText: "Đợt thi đã bị hủy"},
		{name: "target registration closed", pgerr: "REGISTRATION_CLOSED", wantText: "Đã hết thời gian đăng ký của ca thi"},
		{name: "duplicate in target", pgerr: "DUPLICATE_REGISTRATION", wantText: "Bạn đã đăng ký ca thi đích rồi"},
		{name: "no seat in target", pgerr: "NO_SEAT_AVAILABLE", wantText: "Đã hết ghế trống cho ca thi này"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			mock.ExpectQuery("SELECT fn_transfer_registration").
				WithArgs(int64(11), int64(4), int64(7)).
				WillReturnError(errors.New("pq: " + tc.pgerr))

			svc := registration.NewService(db, &fakeAudit{})
			_, err = svc.TransferRegistration(context.Background(), 11, 7, request.TransferRegistrationRequestDTO{NewExamSessionID: 4})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got := err.Error(); got != tc.wantText {
				t.Fatalf("error = %q, want %q", got, tc.wantText)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sqlmock expectations: %v", err)
			}
		})
	}
}
