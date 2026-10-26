package tests

import (
	"context"
	"errors"
	"testing"

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
