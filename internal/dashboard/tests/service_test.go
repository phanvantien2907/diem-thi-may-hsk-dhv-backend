package tests

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/dashboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_GetMetricsOverview(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := dashboard.NewService(db, &fakeAudit{}, nil)

	t.Run("success real-time query", func(t *testing.T) {
		mock.ExpectQuery(`SELECT.*total_revenue,.*total_registrations,.*fill_rate,.*payment_success_rate.*`).
			WillReturnRows(sqlmock.NewRows([]string{"total_revenue", "total_registrations", "fill_rate", "payment_success_rate"}).
				AddRow(1500000, 15, 80.5, 90.0))

		res, err := svc.GetMetricsOverview(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(1500000), res.TotalRevenue)
		assert.Equal(t, int64(15), res.TotalRegistrations)
		assert.Equal(t, float64(80.5), res.FillRate)
		assert.Equal(t, float64(90.0), res.PaymentSuccessRate)
	})

	t.Run("subsequent call queries database directly in real-time", func(t *testing.T) {
		// In real-time mode, every call queries DB directly
		mock.ExpectQuery(`SELECT.*total_revenue,.*total_registrations,.*fill_rate,.*payment_success_rate.*`).
			WillReturnRows(sqlmock.NewRows([]string{"total_revenue", "total_registrations", "fill_rate", "payment_success_rate"}).
				AddRow(2000000, 20, 85.0, 95.0))

		res, err := svc.GetMetricsOverview(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(2000000), res.TotalRevenue)
		assert.Equal(t, int64(20), res.TotalRegistrations)
	})
}

func TestService_ManualApprove(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := dashboard.NewService(db, &fakeAudit{}, nil)

	t.Run("success", func(t *testing.T) {
		mock.ExpectBegin()

		// Lock payment
		mock.ExpectQuery(`SELECT p.payment_status, p.payment_exam_registration_id, er.exam_registration_status, er.exam_registration_exam_seat_id FROM payments p JOIN exam_registrations er`).
			WithArgs(int64(100)).
			WillReturnRows(sqlmock.NewRows([]string{"payment_status", "payment_exam_registration_id", "exam_registration_status", "exam_registration_exam_seat_id"}).
				AddRow("pending", int64(200), "pending_payment", int64(300)))

		// Update payment
		mock.ExpectExec(`UPDATE payments SET payment_status = 'success'`).
			WithArgs(int64(100), `["https://res.cloudinary.com/test.png"]`, "test manual approve").
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Update registration
		mock.ExpectExec(`UPDATE exam_registrations SET exam_registration_status = 'confirmed'`).
			WithArgs(int64(200)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Update seat
		mock.ExpectExec(`UPDATE exam_seats SET exam_seat_status = 'booked'`).
			WithArgs(int64(300)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectCommit()

		res, err := svc.ManualApprove(context.Background(), 100, 1, "test manual approve", []string{"https://res.cloudinary.com/test.png"})
		require.NoError(t, err)
		assert.Equal(t, int64(100), res.PaymentID)
		assert.Equal(t, "success", res.NewStatus)
	})

	t.Run("already success", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT p.payment_status, p.payment_exam_registration_id, er.exam_registration_status, er.exam_registration_exam_seat_id FROM payments p JOIN exam_registrations er`).
			WithArgs(int64(101)).
			WillReturnRows(sqlmock.NewRows([]string{"payment_status", "payment_exam_registration_id", "exam_registration_status", "exam_registration_exam_seat_id"}).
				AddRow("success", int64(201), "confirmed", int64(301)))
		mock.ExpectRollback()

		_, err := svc.ManualApprove(context.Background(), 101, 1, "test manual approve", []string{"https://res.cloudinary.com/test.png"})
		require.Error(t, err)
		assert.True(t, dashboard.IsBusinessError(err))
		assert.Contains(t, err.Error(), "Giao dịch đã được xác nhận")
	})
}

func TestService_SyncPayOSPendingPayments(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := dashboard.NewService(db, &fakeAudit{}, nil)

	t.Run("success updates both", func(t *testing.T) {
		mock.ExpectBegin()

		mock.ExpectExec(`UPDATE payments SET payment_status = 'expired'`).
			WillReturnResult(sqlmock.NewResult(0, 2))

		mock.ExpectExec(`WITH expired_regs AS.*UPDATE exam_seats s SET exam_seat_status = 'available'`).
			WillReturnResult(sqlmock.NewResult(0, 2))

		mock.ExpectCommit()

		count, err := svc.SyncPayOSPendingPayments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("no stale payments", func(t *testing.T) {
		mock.ExpectBegin()

		mock.ExpectExec(`UPDATE payments SET payment_status = 'expired'`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		// No second query should be executed
		mock.ExpectCommit()

		count, err := svc.SyncPayOSPendingPayments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}
