package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/dashboard/dto/response"
	"github.com/hskvinh/backend/internal/utils"
	"github.com/hskvinh/backend/internal/ws"
)

// ─── In-memory cache (no Redis in this project) ─────────────────────────────

type cachedMetrics struct {
	mu        sync.RWMutex
	data      *response.MetricsOverviewResponseDTO
	expiresAt time.Time
}

func (c *cachedMetrics) get() *response.MetricsOverviewResponseDTO {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data != nil && time.Now().Before(c.expiresAt) {
		return c.data
	}
	return nil
}

func (c *cachedMetrics) set(d *response.MetricsOverviewResponseDTO, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = d
	c.expiresAt = time.Now().Add(ttl)
}

func (c *cachedMetrics) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = nil
	c.expiresAt = time.Time{}
}

// Service handles all dashboard analytics and admin operations.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	hub      *ws.Hub
	cache    cachedMetrics
}

// NewService creates a dashboard Service.
func NewService(db *sql.DB, auditSvc audit.Service, hub *ws.Hub) *Service {
	return &Service{
		db:       db,
		auditSvc: auditSvc,
		hub:      hub,
	}
}

const cacheTTL = 3 * time.Minute

// GetMetricsOverview returns top-level KPIs, cached for 3 minutes.
func (s *Service) GetMetricsOverview(ctx context.Context) (response.MetricsOverviewResponseDTO, error) {
	if cached := s.cache.get(); cached != nil {
		return *cached, nil
	}

	var result response.MetricsOverviewResponseDTO
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(p.payment_amount) FILTER (WHERE p.payment_status = 'success'), 0)                AS total_revenue,
			COUNT(DISTINCT er.exam_registration_id)                                                        AS total_registrations,
			CASE WHEN SUM(es.exam_session_capacity) > 0
			     THEN ROUND(COUNT(DISTINCT er.exam_registration_id)::NUMERIC / SUM(DISTINCT es.exam_session_capacity) * 100, 2)
			     ELSE 0 END                                                                                AS fill_rate,
			CASE WHEN COUNT(p.payment_id) > 0
			     THEN ROUND(COUNT(p.payment_id) FILTER (WHERE p.payment_status = 'success')::NUMERIC / COUNT(p.payment_id) * 100, 2)
			     ELSE 0 END                                                                                AS payment_success_rate
		FROM exam_sessions es
		LEFT JOIN exam_registrations er ON er.exam_registration_exam_session_id = es.exam_session_id
		                                AND er.exam_registration_is_deleted = FALSE
		LEFT JOIN payments p ON p.payment_exam_registration_id = er.exam_registration_id
		                      AND p.payment_is_deleted = FALSE
		WHERE es.exam_session_is_deleted = FALSE
		  AND es.exam_session_status = 'open'`).Scan(
		&result.TotalRevenue,
		&result.TotalRegistrations,
		&result.FillRate,
		&result.PaymentSuccessRate,
	)
	if err != nil {
		return response.MetricsOverviewResponseDTO{}, fmt.Errorf("dashboard: metrics overview: %w", err)
	}

	s.cache.set(&result, cacheTTL)
	return result, nil
}

func (s *Service) GetRevenueChart(ctx context.Context, rangeStr string) ([]response.RevenueChartPointDTO, error) {
	days := 7
	if rangeStr == "30days" {
		days = 30
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH date_series AS (
			SELECT generate_series(
				(CURRENT_DATE - ($1 - 1) * INTERVAL '1 day')::DATE,
				CURRENT_DATE,
				'1 day'::INTERVAL
			)::DATE AS d
		)
		SELECT
			ds.d::TEXT                                                                       AS date,
			COALESCE(SUM(p.payment_amount) FILTER (WHERE p.payment_status = 'success'), 0)   AS revenue,
			COUNT(DISTINCT er.exam_registration_id)                                          AS registrations
		FROM date_series ds
		LEFT JOIN payments p ON DATE(p.payment_paid_at) = ds.d
		                      AND p.payment_is_deleted = FALSE
		LEFT JOIN exam_registrations er ON DATE(er.exam_registration_registered_at) = ds.d
		                                AND er.exam_registration_is_deleted = FALSE
		GROUP BY ds.d
		ORDER BY ds.d ASC`, days)
	if err != nil {
		return nil, fmt.Errorf("dashboard: revenue chart: %w", err)
	}
	defer rows.Close()

	items := make([]response.RevenueChartPointDTO, 0, days)
	for rows.Next() {
		var item response.RevenueChartPointDTO
		if err := rows.Scan(&item.Date, &item.Revenue, &item.Registrations); err != nil {
			return nil, fmt.Errorf("dashboard: scan revenue: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetHSKDistribution(ctx context.Context) ([]response.HSKDistributionItemDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT et.exam_type_name, COUNT(er.exam_registration_id)
		FROM exam_registrations er
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		WHERE er.exam_registration_status = 'confirmed'
		  AND er.exam_registration_is_deleted = FALSE
		  AND es.exam_session_is_deleted = FALSE
		GROUP BY et.exam_type_name
		ORDER BY et.exam_type_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: hsk distribution: %w", err)
	}
	defer rows.Close()

	items := make([]response.HSKDistributionItemDTO, 0, 4)
	for rows.Next() {
		var item response.HSKDistributionItemDTO
		if err := rows.Scan(&item.Level, &item.Count); err != nil {
			return nil, fmt.Errorf("dashboard: scan distribution: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetPaymentFunnel(ctx context.Context) ([]response.PaymentFunnelItemDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			EXTRACT(HOUR FROM p.payment_created_at)::INT AS hour,
			COUNT(*) FILTER (WHERE p.payment_status = 'success')        AS success,
			COUNT(*) FILTER (WHERE p.payment_status = 'pending')        AS holding,
			COUNT(*) FILTER (WHERE p.payment_status IN ('failed', 'expired', 'refunded')) AS cancelled
		FROM payments p
		WHERE p.payment_is_deleted = FALSE
		  AND p.payment_created_at >= CURRENT_DATE
		GROUP BY EXTRACT(HOUR FROM p.payment_created_at)
		ORDER BY hour ASC`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: payment funnel: %w", err)
	}
	defer rows.Close()

	items := make([]response.PaymentFunnelItemDTO, 0, 24)
	for rows.Next() {
		var item response.PaymentFunnelItemDTO
		if err := rows.Scan(&item.Hour, &item.Success, &item.Holding, &item.Cancelled); err != nil {
			return nil, fmt.Errorf("dashboard: scan funnel: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetLiveSessionStatus(ctx context.Context) ([]response.LiveSessionItemDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			es.exam_session_id,
			et.exam_type_name,
			et.exam_type_code,
			rm.exam_room_name,
			es.exam_session_date::TEXT,
			es.exam_session_shift,
			COUNT(seat.exam_seat_id)                                             AS total_seats,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'booked')  AS locked_seats,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'held')    AS holding_seats,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'available') AS available_seats
		FROM exam_sessions es
		JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		JOIN exam_rooms rm ON rm.exam_room_id = es.exam_session_exam_room_id
		LEFT JOIN exam_seats seat ON seat.exam_seat_exam_session_id = es.exam_session_id
		                          AND seat.exam_seat_is_deleted = FALSE
		WHERE es.exam_session_is_deleted = FALSE
		  AND es.exam_session_status = 'open'
		GROUP BY es.exam_session_id, et.exam_type_name, et.exam_type_code,
		         rm.exam_room_name, es.exam_session_date, es.exam_session_shift
		ORDER BY es.exam_session_date ASC, es.exam_session_shift ASC`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: live status: %w", err)
	}
	defer rows.Close()

	items := make([]response.LiveSessionItemDTO, 0, 16)
	for rows.Next() {
		var item response.LiveSessionItemDTO
		if err := rows.Scan(
			&item.SessionID, &item.ExamTypeName, &item.ExamTypeCode,
			&item.RoomName, &item.SessionDate, &item.Shift,
			&item.TotalSeats, &item.LockedSeats, &item.HoldingSeats, &item.AvailableSeats,
		); err != nil {
			return nil, fmt.Errorf("dashboard: scan live status: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetTransactionExceptions(ctx context.Context, page, limit int) ([]response.TransactionExceptionItemDTO, *utils.Meta, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Count total exceptions
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		WHERE p.payment_is_deleted = FALSE
		  AND er.exam_registration_is_deleted = FALSE
		  AND (
		    p.payment_status = 'failed'
		    OR (p.payment_status = 'pending' AND p.payment_created_at < NOW() - INTERVAL '15 minutes')
		  )`).Scan(&total)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard: count exceptions: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.payment_id,
			er.exam_registration_id,
			COALESCE(c.candidate_full_name, a.account_username),
			es.exam_session_fee,
			p.payment_amount,
			p.payment_status,
			CASE
				WHEN p.payment_status = 'failed' THEN 'webhook_failed'
				WHEN p.payment_status = 'pending' AND p.payment_created_at < NOW() - INTERVAL '15 minutes' THEN 'expired_payment'
				ELSE 'amount_mismatch'
			END AS exception_type,
			p.payment_transaction_ref,
			to_char(p.payment_created_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYY-MM-DD"T"HH24:MI:SS+07:00')
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		JOIN accounts a ON a.account_id = c.candidate_account_id
		WHERE p.payment_is_deleted = FALSE
		  AND er.exam_registration_is_deleted = FALSE
		  AND (
		    p.payment_status = 'failed'
		    OR (p.payment_status = 'pending' AND p.payment_created_at < NOW() - INTERVAL '15 minutes')
		  )
		ORDER BY p.payment_created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard: exceptions: %w", err)
	}
	defer rows.Close()

	items := make([]response.TransactionExceptionItemDTO, 0, limit)
	for rows.Next() {
		var item response.TransactionExceptionItemDTO
		var actualAmount int64
		if err := rows.Scan(
			&item.PaymentID, &item.RegistrationID, &item.CandidateName,
			&item.ExpectedAmount, &actualAmount, &item.Status,
			&item.ExceptionType, &item.TransactionRef, &item.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("dashboard: scan exception: %w", err)
		}
		item.ActualAmount = &actualAmount
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("dashboard: exception rows: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return items, meta, nil
}

func (s *Service) ManualApprove(ctx context.Context, paymentID, adminID int64, reason string) (response.ManualApproveResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Lock payment row
	var status, regStatus string
	var regID, seatID int64
	err = tx.QueryRowContext(ctx, `
		SELECT p.payment_status, p.payment_exam_registration_id,
		       er.exam_registration_status, er.exam_registration_exam_seat_id
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		WHERE p.payment_id = $1
		  AND p.payment_is_deleted = FALSE
		  AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF p, er`, paymentID).Scan(&status, &regID, &regStatus, &seatID)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ManualApproveResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	}
	if err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: lock payment: %w", err)
	}

	if status == "success" {
		return response.ManualApproveResponseDTO{}, errors.New("Giao dịch đã được xác nhận trước đó")
	}
	if regStatus == "cancelled" {
		return response.ManualApproveResponseDTO{}, errors.New("Phiếu đăng ký đã bị hủy, không thể duyệt")
	}

	// Update payment → success
	if _, err := tx.ExecContext(ctx,
		`UPDATE payments SET payment_status = 'success', payment_paid_at = NOW()
		 WHERE payment_id = $1`, paymentID); err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: update payment: %w", err)
	}

	// Confirm registration
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_registrations SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
		 WHERE exam_registration_id = $1`, regID); err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: confirm reg: %w", err)
	}

	// Book seat
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		 WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE`, seatID); err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: book seat: %w", err)
	}

	// Audit log
	oldVal, _ := json.Marshal(map[string]any{"status": status, "reg_status": regStatus})
	newVal, _ := json.Marshal(map[string]any{"status": "success", "reg_status": "confirmed", "reason": reason, "action": "manual_approve"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    auditenum.AuditEntityPayment.String(),
		EntityID:       paymentID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.ManualApproveResponseDTO{}, fmt.Errorf("dashboard: commit: %w", err)
	}

	s.cache.invalidate()

	if s.hub != nil {
		s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
			"registration_id": regID,
			"seat_id":         seatID,
			"status":          "confirmed",
			"action":          "manual_approve",
		})
	}

	return response.ManualApproveResponseDTO{
		PaymentID:      paymentID,
		RegistrationID: regID,
		NewStatus:      "success",
		Message:        "Đã duyệt thủ công thành công",
	}, nil
}

func (s *Service) RefundMark(ctx context.Context, paymentID, adminID int64, reason string) (response.RefundMarkResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.RefundMarkResponseDTO{}, fmt.Errorf("dashboard: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT payment_status FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, paymentID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return response.RefundMarkResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	}
	if err != nil {
		return response.RefundMarkResponseDTO{}, fmt.Errorf("dashboard: lock payment: %w", err)
	}
	if status == "refunded" {
		return response.RefundMarkResponseDTO{}, errors.New("Giao dịch đã được đánh dấu hoàn tiền trước đó")
	}
	if status != "success" && status != "pending" {
		return response.RefundMarkResponseDTO{}, errors.New("Chỉ có thể đánh dấu hoàn tiền cho giao dịch thành công hoặc đang chờ")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE payments SET payment_status = 'refunded' WHERE payment_id = $1`, paymentID); err != nil {
		return response.RefundMarkResponseDTO{}, fmt.Errorf("dashboard: update refund: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": status})
	newVal, _ := json.Marshal(map[string]any{"status": "refunded", "reason": reason, "action": "refund_mark"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionRefund.String(),
		EntityTable:    auditenum.AuditEntityPayment.String(),
		EntityID:       paymentID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.RefundMarkResponseDTO{}, fmt.Errorf("dashboard: commit: %w", err)
	}

	s.cache.invalidate()

	return response.RefundMarkResponseDTO{
		PaymentID: paymentID,
		NewStatus: "refunded",
		Message:   "Đã đánh dấu hoàn tiền thành công",
	}, nil
}

func (s *Service) GetRecentLogs(ctx context.Context, page, limit int) ([]response.RecentLogItemDTO, *utils.Meta, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("dashboard: count logs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT audit_log_id, audit_log_action, audit_log_entity_table,
		       audit_log_entity_id, audit_log_actor_account_id,
		       to_char(audit_log_created_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYY-MM-DD"T"HH24:MI:SS+07:00')
		FROM audit_logs
		ORDER BY audit_log_created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard: recent logs: %w", err)
	}
	defer rows.Close()

	items := make([]response.RecentLogItemDTO, 0, limit)
	for rows.Next() {
		var item response.RecentLogItemDTO
		if err := rows.Scan(&item.ID, &item.Action, &item.EntityTable, &item.EntityID, &item.ActorID, &item.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("dashboard: scan log: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("dashboard: log rows: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return items, meta, nil
}

func (s *Service) ClearCache(ctx context.Context, adminID int64) (response.ClearCacheResponseDTO, error) {
	s.cache.invalidate()

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       adminID,
		NewValue:       []byte(`{"action":"clear_cache"}`),
	})

	log.Printf("[dashboard] cache cleared by admin_id=%d", adminID)
	return response.ClearCacheResponseDTO{
		Message: "Đã xóa bộ nhớ đệm thành công",
	}, nil
}

func (s *Service) GetMyAuditLog(ctx context.Context, adminID int64, page, limit int) ([]response.AdminAuditLogItemDTO, *utils.Meta, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE audit_log_actor_account_id = $1`, adminID,
	).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("dashboard: count my logs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT audit_log_id, audit_log_action, audit_log_entity_table,
		       audit_log_entity_id, audit_log_old_value, audit_log_new_value,
		       to_char(audit_log_created_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYY-MM-DD"T"HH24:MI:SS+07:00')
		FROM audit_logs
		WHERE audit_log_actor_account_id = $1
		ORDER BY audit_log_created_at DESC
		LIMIT $2 OFFSET $3`, adminID, limit, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard: my audit log: %w", err)
	}
	defer rows.Close()

	items := make([]response.AdminAuditLogItemDTO, 0, limit)
	for rows.Next() {
		var item response.AdminAuditLogItemDTO
		var oldVal, newVal sql.NullString
		if err := rows.Scan(&item.ID, &item.Action, &item.EntityTable, &item.EntityID, &oldVal, &newVal, &item.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("dashboard: scan my audit: %w", err)
		}
		if oldVal.Valid {
			raw := json.RawMessage(oldVal.String)
			item.OldValue = &raw
		}
		if newVal.Valid {
			raw := json.RawMessage(newVal.String)
			item.NewValue = &raw
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("dashboard: my audit rows: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return items, meta, nil
}

func (s *Service) SyncPayOSPendingPayments(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("dashboard: sync payos: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'expired'
		WHERE payment_status = 'pending'
		  AND payment_is_deleted = FALSE
		  AND payment_created_at < NOW() - INTERVAL '15 minutes'
		  AND payment_transaction_ref IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("dashboard: sync payos: expire payments: %w", err)
	}
	count, _ := res.RowsAffected()

	if count > 0 {
		_, err = tx.ExecContext(ctx, `
			WITH expired_regs AS (
				SELECT DISTINCT p.payment_exam_registration_id
				FROM payments p
				WHERE p.payment_status = 'expired'
				  AND p.payment_is_deleted = FALSE
				  AND p.payment_created_at >= NOW() - INTERVAL '16 minutes'
				  AND p.payment_created_at < NOW() - INTERVAL '15 minutes'
			)
			UPDATE exam_seats s
			SET exam_seat_status = 'available', exam_seat_held_until = NULL
			FROM exam_registrations er
			WHERE er.exam_registration_exam_seat_id = s.exam_seat_id
			  AND er.exam_registration_id IN (SELECT payment_exam_registration_id FROM expired_regs)
			  AND er.exam_registration_status = 'pending_payment'
			  AND s.exam_seat_status = 'held'
			  AND s.exam_seat_is_deleted = FALSE`)
		if err != nil {
			log.Printf("[dashboard] sync payos: release seats error: %v", err)
			return 0, fmt.Errorf("dashboard: sync payos: release seats: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("dashboard: sync payos: commit: %w", err)
	}

	if count > 0 {
		s.cache.invalidate()

		if s.hub != nil {
			s.hub.Broadcast(ws.EventDashboardMetricsUpdated, map[string]interface{}{
				"expired_count": count,
				"message":       "Giao dịch quá hạn đã được cập nhật",
			})
		}
	}

	return count, nil
}

func IsBusinessError(err error) bool {
	msg := err.Error()
	businessPrefixes := []string{
		"Không tìm thấy",
		"Giao dịch đã",
		"Phiếu đăng ký đã",
		"Chỉ có thể",
	}
	for _, p := range businessPrefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}
