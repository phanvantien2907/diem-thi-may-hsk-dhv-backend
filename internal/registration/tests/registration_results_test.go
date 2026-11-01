package tests

import (
	"bytes"
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
	"github.com/lib/pq"

	payreq "github.com/hskvinh/backend/internal/payment/dto/request"
	payresp "github.com/hskvinh/backend/internal/payment/dto/response"
	"github.com/hskvinh/backend/internal/registration"
	"github.com/hskvinh/backend/internal/registration/dto/request"
)

type fakePaymentCanceller struct {
	calledPaymentID int64
	calledActorID   int64
	calledReq       payreq.AdminCancelPaymentRequestDTO
	resp            payresp.AdminPaymentResponseDTO
	err             error
}

func (f *fakePaymentCanceller) AdminCancelPaymentAs(_ context.Context, paymentID, actorID int64, req payreq.AdminCancelPaymentRequestDTO) (payresp.AdminPaymentResponseDTO, error) {
	f.calledPaymentID = paymentID
	f.calledActorID = actorID
	f.calledReq = req
	return f.resp, f.err
}

var slotStatsMockColumns = []string{
	"exam_session_id",
	"exam_session_exam_period_id",
	"exam_session_exam_type_id",
	"exam_type_code",
	"exam_type_name",
	"exam_session_exam_room_id",
	"exam_room_name",
	"exam_session_date_text",
	"exam_session_date",
	"exam_session_shift",
	"exam_session_fee",
	"exam_session_status",
	"exam_session_closed_reason",
	"exam_session_publish_at",
	"exam_session_registration_deadline",
	"exam_session_capacity",
	"exam_period_status",
	"exam_period_registration_start",
	"exam_period_registration_deadline",
	"exam_period_end_date",
	"confirmed_count",
	"pending_payment_count",
	"cancelled_count",
	"attended_count",
	"booked_seats",
	"held_seats",
	"available_seats",
	"waitlist_count",
	"single_slot_count",
	"multi_slot_count",
	"eligible_count",
	"ineligible_count",
	"pending_review_count",
	"unpaid_confirmed_count",
}

func TestGetPeriodRegistrationResults_GroupedSlotsAndAlerts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now()
	nearExamDate := now.AddDate(0, 0, 3) // 3 days from now -> triggers near-exam alerts
	periodStart := now.AddDate(0, 0, -5)
	periodEnd := now.AddDate(0, 0, 10)
	regStart := now.AddDate(0, 0, -5)
	regDeadline := now.AddDate(0, 0, 2)

	mock.ExpectQuery(`SELECT ep\.exam_period_id, ep\.exam_period_code`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_period_id", "exam_period_code", "exam_period_name", "exam_period_status",
			"start_date_text", "end_date_text", "start_date", "end_date", "reg_start", "reg_deadline",
		}).AddRow(
			int64(10), "DOT-2026-11", "Đợt thi tháng 11/2026", "registration_open",
			periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"),
			periodStart, periodEnd, regStart, regDeadline,
		))

	slotRows := sqlmock.NewRows(slotStatsMockColumns).
		// Slot 101: 90/100 capacity (90% -> nearly_full), confirmed=85 vs booked=84 (discrepancy), ineligible=2 near exam
		AddRow(
			int64(101), int64(10), int64(3), "HSK3", "HSK Cấp 3", int64(1), "Phòng máy A1",
			nearExamDate.Format("2006-01-02"), nearExamDate, "Sáng (08:00)", int64(750000),
			"open", "", nil, regDeadline, 100,
			"registration_open", regStart, regDeadline, periodEnd,
			85, 5, 2, 0,
			84, 5, 11,
			4,
			70, 20,
			83, 2, 5,
			0,
		).
		// Slot 102: 3/50 capacity near exam date -> too_few_registrants
		AddRow(
			int64(102), int64(10), int64(4), "HSK4", "HSK Cấp 4", int64(2), "Phòng máy A2",
			nearExamDate.Format("2006-01-02"), nearExamDate, "Chiều (13:30)", int64(900000),
			"open", "", nil, regDeadline, 50,
			"registration_open", regStart, regDeadline, periodEnd,
			3, 0, 0, 0,
			3, 0, 47,
			0,
			3, 0,
			3, 0, 0,
			0,
		)

	mock.ExpectQuery(`WITH latest_docs AS`).
		WithArgs(int64(10)).
		WillReturnRows(slotRows)

	svc := registration.NewService(db, &fakeAudit{})
	summary, err := svc.GetPeriodRegistrationResults(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary.PeriodID != 10 || summary.TotalSlots != 2 {
		t.Fatalf("got period_id=%d total_slots=%d, want 10 and 2", summary.PeriodID, summary.TotalSlots)
	}
	if summary.TotalCapacity != 150 || summary.TotalConfirmed != 88 {
		t.Fatalf("got capacity=%d confirmed=%d, want 150 and 88", summary.TotalCapacity, summary.TotalConfirmed)
	}
	if summary.DiscrepancyCount != 1 {
		t.Fatalf("got discrepancy_count=%d, want 1", summary.DiscrepancyCount)
	}
	if summary.MultiSlotCandidates != 20 {
		t.Fatalf("got multi_slot_candidates=%d, want 20", summary.MultiSlotCandidates)
	}

	// Verify alert types generated across the period
	alertTypes := make(map[string]bool)
	for _, a := range summary.Alerts {
		alertTypes[a.Type] = true
	}
	for _, expectedType := range []string{"seat_discrepancy", "nearly_full", "ineligible_docs_near_exam", "too_few_registrants"} {
		if !alertTypes[expectedType] {
			t.Errorf("expected alert type %q to be present in %+v", expectedType, summary.Alerts)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListSlotRegistrations_MasksPII_DetectsMultiSlot_AndBuildsShortcuts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`WITH latest_docs AS .* SELECT COUNT\(\*\)`).
		WithArgs(int64(101)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	regAt := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	dob := time.Date(2002, 5, 15, 0, 0, 0, 0, time.UTC)
	rejReason := "Ảnh chân dung bị mờ"

	rows := sqlmock.NewRows([]string{
		"exam_registration_id", "exam_registration_exam_session_id", "exam_session_exam_period_id",
		"exam_registration_exam_seat_id", "exam_seat_number", "exam_seat_status",
		"exam_registration_status", "exam_registration_registered_at", "exam_registration_is_attended",
		"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_full_name_cn",
		"candidate_dob", "candidate_gender", "account_phone", "account_email",
		"doc_id", "doc_type", "doc_number", "verification_status", "rejection_reason",
		"has_portrait_photo", "has_identity_photos",
		"payment_id", "payment_status", "payment_amount",
		"slot_count", "session_ids",
	}).
		// Candidate 1: Multi-slot (2 sessions), eligible
		AddRow(
			int64(501), int64(101), int64(10),
			int64(1001), "A01", "booked",
			"confirmed", regAt, false,
			int64(77), int64(88), "Nguyễn Văn An", "阮文安",
			dob, "nam", "0912345678", "an@example.com",
			int64(301), "cccd", "040202001234", "verified", nil,
			true, true,
			int64(901), "success", int64(750000),
			2, pq.Array([]int64{101, 102}),
		).
		// Candidate 2: Single-slot, rejected document -> ineligible
		AddRow(
			int64(502), int64(101), int64(10),
			int64(1002), "A02", "held",
			"pending_payment", regAt, false,
			int64(78), int64(89), "Trần Thị Bình", nil,
			dob, "nữ", "0987654321", "binh@example.com",
			int64(302), "cccd", "038199009876", "rejected", rejReason,
			true, true,
			int64(902), "pending", int64(750000),
			1, pq.Array([]int64{101}),
		)

	mock.ExpectQuery(`WITH latest_docs AS .* SELECT er\.exam_registration_id`).
		WithArgs(int64(101), 20, 0).
		WillReturnRows(rows)

	svc := registration.NewService(db, &fakeAudit{})
	items, meta, err := svc.ListSlotRegistrations(context.Background(), 101, request.AdminListSlotRegistrationsRequestDTO{
		Page:  1,
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.TotalItems != 2 || len(items) != 2 {
		t.Fatalf("got len=%d total=%d, want 2", len(items), meta.TotalItems)
	}

	// Verify Candidate 1: PII masked, multi-slot detected, eligible, shortcuts populated
	c1 := items[0]
	if c1.MaskedPhone != "091***678" {
		t.Errorf("c1 MaskedPhone = %q, want 091***678", c1.MaskedPhone)
	}
	if c1.MaskedDocNumber != "040***234" {
		t.Errorf("c1 MaskedDocNumber = %q, want 040***234", c1.MaskedDocNumber)
	}
	if !c1.IsMultiSlot || c1.RegisteredSlotCount != 2 || len(c1.RegisteredSessionIDs) != 2 {
		t.Errorf("c1 multi-slot = %v count=%d sessions=%v", c1.IsMultiSlot, c1.RegisteredSlotCount, c1.RegisteredSessionIDs)
	}
	if c1.EligibilityStatus != "eligible" {
		t.Errorf("c1 EligibilityStatus = %q, want eligible", c1.EligibilityStatus)
	}
	if c1.ProfileShortcuts.DetailAndAcquireLockURL != "/api/v1/admin/candidates/77" ||
		c1.ProfileShortcuts.ApproveDocumentURL != "/api/v1/admin/candidates/77/approve" ||
		c1.ProfileShortcuts.RejectDocumentURL != "/api/v1/admin/candidates/77/reject" {
		t.Errorf("unexpected shortcut links: %+v", c1.ProfileShortcuts)
	}

	// Verify Candidate 2: Single-slot, ineligible with rejection reason
	c2 := items[1]
	if c2.IsMultiSlot || c2.RegisteredSlotCount != 1 {
		t.Errorf("c2 expected single slot, got IsMultiSlot=%v count=%d", c2.IsMultiSlot, c2.RegisteredSlotCount)
	}
	if c2.EligibilityStatus != "ineligible" {
		t.Errorf("c2 EligibilityStatus = %q, want ineligible", c2.EligibilityStatus)
	}
	if len(c2.EligibilityReasons) == 0 || !strings.Contains(c2.EligibilityReasons[0], "Ảnh chân dung bị mờ") {
		t.Errorf("c2 EligibilityReasons = %v, expected rejection reason", c2.EligibilityReasons)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminCancelRegistration_DelegatesPaidRegistrationToRefundWorkflow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT er\.exam_registration_exam_session_id`).
		WithArgs(int64(501)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_exam_session_id",
			"exam_registration_exam_seat_id",
			"exam_registration_status",
			"payment_id",
			"payment_status",
		}).AddRow(int64(101), int64(1001), "confirmed", int64(901), "success"))

	refundAmt := int64(750000)
	fakePay := &fakePaymentCanceller{
		resp: payresp.AdminPaymentResponseDTO{
			ID:             901,
			RegistrationID: 501,
			Status:         "pending_refund",
			RefundAmount:   &refundAmt,
		},
	}

	svc := registration.NewService(db, &fakeAudit{})
	svc.SetPaymentCanceller(fakePay)

	res, err := svc.AdminCancelRegistration(context.Background(), 501, 1, request.AdminCancelRegistrationRequestDTO{
		Reason: "Thí sinh xin hủy đăng ký trước ngày thi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakePay.calledPaymentID != 901 || fakePay.calledActorID != 1 {
		t.Fatalf("expected delegation to paymentID=901 actorID=1, got %d and %d", fakePay.calledPaymentID, fakePay.calledActorID)
	}
	if res.Status != "cancelled" || res.PaymentStatus != "pending_refund" {
		t.Fatalf("got status=%q payment_status=%q, want cancelled and pending_refund", res.Status, res.PaymentStatus)
	}
	if res.RefundAmount == nil || *res.RefundAmount != 750000 {
		t.Fatalf("expected refund amount 750000, got %v", res.RefundAmount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminCancelRegistration_UnpaidNoPaymentRecord_ReleasesSeatAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	futureExam := time.Now().AddDate(0, 0, 10)

	mock.ExpectQuery(`SELECT er\.exam_registration_exam_session_id`).
		WithArgs(int64(505)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_exam_session_id",
			"exam_registration_exam_seat_id",
			"exam_registration_status",
			"payment_id",
			"payment_status",
		}).AddRow(int64(101), int64(1005), "pending_payment", nil, nil))

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT er\.exam_registration_exam_seat_id, er\.exam_registration_status`).
		WithArgs(int64(505)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_exam_seat_id", "exam_registration_status", "exam_session_date", "exam_passed",
		}).AddRow(int64(1005), "pending_payment", futureExam, false))

	mock.ExpectExec(`UPDATE exam_registrations SET exam_registration_status = 'cancelled'`).
		WithArgs(int64(505)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`UPDATE exam_seats SET exam_seat_status = 'available'`).
		WithArgs(int64(1005)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	svc := registration.NewService(db, &fakeAudit{})
	res, err := svc.AdminCancelRegistration(context.Background(), 505, 1, request.AdminCancelRegistrationRequestDTO{
		Reason: "Hủy đơn giữ chỗ chưa tạo link thanh toán",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "cancelled" || res.PaymentStatus != "none" {
		t.Fatalf("got status=%q payment_status=%q", res.Status, res.PaymentStatus)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminTransferRegistration_UsesStoredFunctionAndPreservesRegistration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	regAt := time.Now()

	mock.ExpectQuery(`SELECT exam_registration_candidate_id, exam_registration_exam_session_id, exam_registration_exam_seat_id`).
		WithArgs(int64(501)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_candidate_id", "exam_registration_exam_session_id", "exam_registration_exam_seat_id",
		}).AddRow(int64(77), int64(101), int64(1001)))

	mock.ExpectQuery(`SELECT fn_transfer_registration\(\$1, \$2, \$3, 15\)`).
		WithArgs(int64(501), int64(102), int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{"fn_transfer_registration"}).AddRow(int64(501)))

	mock.ExpectQuery(`SELECT exam_registration_id, exam_registration_candidate_id`).
		WithArgs(int64(501)).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_registration_id", "exam_registration_candidate_id",
			"exam_registration_exam_session_id", "exam_registration_exam_seat_id",
			"exam_registration_status", "exam_registration_registered_at",
		}).AddRow(int64(501), int64(77), int64(102), int64(2005), "confirmed", regAt))

	svc := registration.NewService(db, &fakeAudit{})
	dto, err := svc.AdminTransferRegistration(context.Background(), 501, 1, request.AdminTransferRegistrationRequestDTO{
		NewExamSessionID: 102,
		Reason:           "Điều chuyển sang ca chiều theo đơn đề nghị",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.SessionID != 102 || dto.SeatID != 2005 || dto.Status != "confirmed" {
		t.Fatalf("unexpected transferred DTO: %+v", dto)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminListWaitlist_ReturnsQueuePositionAndMasksPII(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM exam_waitlists`).
		WithArgs(int64(101)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	joined1 := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	joined2 := time.Date(2026, 10, 30, 8, 5, 0, 0, time.UTC)
	dob := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`WITH latest_docs AS .* ranked_waitlist AS`).
		WithArgs(int64(101), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"exam_waitlist_id", "exam_waitlist_exam_session_id", "queue_position", "exam_waitlist_joined_at",
			"candidate_id", "candidate_account_id", "candidate_full_name", "candidate_dob", "candidate_gender",
			"account_phone", "account_email", "doc_number", "verification_status", "slot_count",
		}).
			AddRow(int64(1), int64(101), 1, joined1, int64(81), int64(91), "Lê Văn C", dob, "nam", "0901112233", "c@example.com", "040111222333", "verified", 0).
			AddRow(int64(2), int64(101), 2, joined2, int64(82), int64(92), "Phạm Thị D", dob, "nữ", "0904445566", "d@example.com", "040444555666", "pending", 2))

	svc := registration.NewService(db, &fakeAudit{})
	list, meta, err := svc.AdminListWaitlist(context.Background(), 101, request.AdminListWaitlistRequestDTO{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.TotalItems != 2 || len(list) != 2 {
		t.Fatalf("got total=%d len=%d, want 2", meta.TotalItems, len(list))
	}
	if list[0].QueuePosition != 1 || list[1].QueuePosition != 2 {
		t.Errorf("queue positions = %d, %d; want 1, 2", list[0].QueuePosition, list[1].QueuePosition)
	}
	if list[0].MaskedPhone != "090***233" || list[0].MaskedDocNumber != "040***333" {
		t.Errorf("PII not masked: phone=%q doc=%q", list[0].MaskedPhone, list[0].MaskedDocNumber)
	}
	if !list[1].IsMultiSlot {
		t.Errorf("expected list[1].IsMultiSlot = true")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminCheckDiscrepancies_FiltersOnlyMismatchedSlots(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	examDate := time.Now().AddDate(0, 0, 14)

	rows := sqlmock.NewRows(slotStatsMockColumns).
		// Slot 101: Clean (confirmed=20 == booked=20, pending=2 == held=2, unpaid_confirmed=0)
		AddRow(
			int64(101), int64(10), int64(3), "HSK3", "HSK Cấp 3", int64(1), "Phòng A1",
			examDate.Format("2006-01-02"), examDate, "Sáng", int64(750000),
			"open", "", nil, examDate, 50,
			"registration_open", nil, nil, nil,
			20, 2, 0, 0,
			20, 2, 28,
			0, 22, 0, 20, 0, 2, 0,
		).
		// Slot 102: Discrepancy (confirmed=15 != booked=14)
		AddRow(
			int64(102), int64(10), int64(4), "HSK4", "HSK Cấp 4", int64(2), "Phòng A2",
			examDate.Format("2006-01-02"), examDate, "Chiều", int64(900000),
			"open", "", nil, examDate, 50,
			"registration_open", nil, nil, nil,
			15, 0, 0, 0,
			14, 0, 36,
			0, 15, 0, 15, 0, 0, 0,
		)

	mock.ExpectQuery(`WITH latest_docs AS`).
		WithArgs(int64(10)).
		WillReturnRows(rows)

	svc := registration.NewService(db, &fakeAudit{})
	discrepancies, err := svc.AdminCheckDiscrepancies(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(discrepancies) != 1 {
		t.Fatalf("got %d discrepancies, want 1", len(discrepancies))
	}
	if discrepancies[0].SessionID != 102 || discrepancies[0].ConfirmedSeatDiff != 1 {
		t.Errorf("unexpected discrepancy: %+v", discrepancies[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminExportRegistrations_CSVAndPDF(t *testing.T) {
	for _, format := range []string{"excel", "pdf"} {
		t.Run(format, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock: %v", err)
			}
			defer db.Close()

			dob := time.Date(2000, 8, 20, 0, 0, 0, 0, time.UTC)
			regAt := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

			rows := sqlmock.NewRows([]string{
				"exam_registration_id", "exam_period_name", "exam_session_id", "exam_type_name",
				"exam_session_date", "exam_session_shift", "exam_room_name", "exam_seat_number",
				"candidate_full_name", "candidate_full_name_cn", "candidate_dob", "candidate_gender",
				"account_phone", "account_email", "doc_type", "doc_number",
				"verification_status", "rejection_reason", "has_portrait_photo", "has_identity_photos",
				"exam_registration_status", "payment_status", "slot_count", "exam_registration_registered_at",
			}).AddRow(
				int64(501), "Đợt thi tháng 11", int64(101), "HSK Cấp 3",
				"2026-11-15", "Sáng", "Phòng A1", "A01",
				"Nguyễn Văn An", "阮文安", dob, "nam",
				"0912345678", "an@example.com", "cccd", "040202001234",
				"verified", "", true, true,
				"confirmed", "success", 2, regAt,
			)

			mock.ExpectQuery(`WITH latest_docs AS .* SELECT er\.exam_registration_id`).
				WithArgs(int64(101)).
				WillReturnRows(rows)

			svc := registration.NewService(db, &fakeAudit{})
			data, contentType, filename, err := svc.AdminExportRegistrations(context.Background(), 1, request.AdminExportRegistrationsRequestDTO{
				SessionID: 101,
				Format:    format,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(data) == 0 {
				t.Fatal("expected non-empty exported bytes")
			}

			if format == "excel" {
				if !strings.HasPrefix(contentType, "text/csv") || !strings.HasSuffix(filename, ".csv") {
					t.Errorf("got contentType=%q filename=%q", contentType, filename)
				}
				// Verify UTF-8 BOM and masked PII in CSV
				if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
					t.Error("expected UTF-8 BOM at beginning of CSV")
				}
				csvStr := string(data)
				if !strings.Contains(csvStr, "091***678") || !strings.Contains(csvStr, "040***234") {
					t.Errorf("expected masked PII in CSV output, got: %s", csvStr)
				}
			} else {
				if contentType != "application/pdf" || !strings.HasSuffix(filename, ".pdf") {
					t.Errorf("got contentType=%q filename=%q", contentType, filename)
				}
				if !bytes.HasPrefix(data, []byte("%PDF")) {
					t.Error("expected valid %PDF header")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestHandler_RegistrationResults_Validation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := registration.NewHandler(registration.NewService(nil, &fakeAudit{}))

	t.Run("GetPeriodRegistrationResults invalid period_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "period_id", Value: "abc"}}
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/registration-results/periods/abc", nil)

		h.GetPeriodRegistrationResults(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("AdminExportRegistrations missing format", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", int64(1))
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/registration-results/export?session_id=101", nil)

		h.AdminExportRegistrations(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("AdminCancelRegistration short reason", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", int64(1))
		c.Params = gin.Params{{Key: "id", Value: "501"}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/registrations/501/cancel", strings.NewReader(`{"reason":"ab"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.AdminCancelRegistration(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("AdminTransferRegistration invalid target session", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", int64(1))
		c.Params = gin.Params{{Key: "id", Value: "501"}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/registrations/501/transfer", strings.NewReader(`{"new_exam_session_id":0}`))
		c.Request.Header.Set("Content-Type", "application/json")

		h.AdminTransferRegistration(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["statusCode"] != float64(400) {
			t.Errorf("expected statusCode=400, got %v", resp)
		}
	})

	t.Run("AdminTransferRegistration maps duplicate target session error", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()

		mock.ExpectQuery(`SELECT exam_registration_candidate_id`).
			WithArgs(int64(501)).
			WillReturnRows(sqlmock.NewRows([]string{"exam_registration_candidate_id", "exam_registration_exam_session_id", "exam_registration_exam_seat_id"}).
				AddRow(int64(77), int64(101), int64(1001)))
		mock.ExpectQuery(`SELECT fn_transfer_registration`).
			WithArgs(int64(501), int64(102), int64(77)).
			WillReturnError(errors.New("pq: DUPLICATE_REGISTRATION"))

		hWithDB := registration.NewHandler(registration.NewService(db, &fakeAudit{}))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", int64(1))
		c.Params = gin.Params{{Key: "id", Value: "501"}}
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/registrations/501/transfer", strings.NewReader(`{"new_exam_session_id":102}`))
		c.Request.Header.Set("Content-Type", "application/json")

		hWithDB.AdminTransferRegistration(c)
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409; body=%s", w.Code, w.Body.String())
		}
	})
}
