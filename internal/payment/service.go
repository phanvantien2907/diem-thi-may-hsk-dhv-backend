package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/payment/dto/request"
	"github.com/hskvinh/backend/internal/payment/dto/response"
	payenum "github.com/hskvinh/backend/internal/payment/enum"
	regenum "github.com/hskvinh/backend/internal/registration/enum"
	"github.com/hskvinh/backend/internal/utils"
	"github.com/hskvinh/backend/internal/ws"
)

// PayOSConfig holds the credentials needed to call PayOS API.
type PayOSConfig struct {
	ClientID    string
	ApiKey      string
	ChecksumKey string
}

// Service handles payment creation and confirmation.
type Service struct {
	db       *sql.DB
	auditSvc audit.Service
	payOS    PayOSConfig
	httpCli  *http.Client
	hub      *ws.Hub // optional, nil-safe
}

// NewService creates a payment Service.
func NewService(db *sql.DB, auditSvc audit.Service, payOS PayOSConfig) *Service {
	return &Service{
		db:       db,
		auditSvc: auditSvc,
		payOS:    payOS,
		httpCli:  &http.Client{Timeout: 15 * time.Second},
	}
}

// SetHub attaches a WebSocket hub for broadcasting real-time events.
func (s *Service) SetHub(hub *ws.Hub) { s.hub = hub }

// ─── Column list + scanner (DRY) ───────────────────────────────────────────

const paymentCols = `payment_id, payment_exam_registration_id, payment_amount, payment_method,
	payment_gateway, payment_transaction_ref, payment_gateway_trans_id, payment_content,
	payment_status, payment_expires_at, payment_paid_at,
	payment_is_deleted, payment_deleted_at, payment_created_at, payment_updated_at`

const adminPaymentCols = `p.payment_id, p.payment_exam_registration_id, p.payment_amount,
	p.payment_amount_received, p.payment_method, p.payment_gateway, p.payment_transaction_ref,
	p.payment_gateway_trans_id, p.payment_content, p.payment_status, p.payment_expires_at,
	p.payment_paid_at, p.payment_is_deleted, p.payment_deleted_at, p.payment_created_at,
	p.payment_updated_at`

func scanPayment(row interface{ Scan(dest ...any) error }, e *PaymentEntity) error {
	return row.Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID, &e.PaymentContent,
		&e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
}

func scanAdminPayment(row interface{ Scan(dest ...any) error }, e *PaymentEntity) error {
	return row.Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentAmountReceived,
		&e.PaymentMethod, &e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID,
		&e.PaymentContent, &e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
}

// fetchPaymentByID loads a single payment by ID (reused by admin confirm/cancel after procedure call).
func (s *Service) fetchPaymentByID(ctx context.Context, id int64) (PaymentEntity, error) {
	var e PaymentEntity
	err := s.db.QueryRowContext(ctx,
		`SELECT `+paymentCols+` FROM payments WHERE payment_id = $1 AND payment_is_deleted = FALSE`, id,
	).Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID, &e.PaymentContent,
		&e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
	return e, err
}

func (s *Service) fetchAdminPaymentByID(ctx context.Context, id int64) (PaymentEntity, error) {
	var e PaymentEntity
	err := s.db.QueryRowContext(ctx,
		`SELECT `+adminPaymentCols+` FROM payments p WHERE p.payment_id = $1 AND p.payment_is_deleted = FALSE`, id,
	).Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentAmountReceived,
		&e.PaymentMethod, &e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID,
		&e.PaymentContent, &e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
	return e, err
}

// ─── UC1: Candidate creates a PayOS payment link ────────────────────────────

// CreatePayOSLink creates a pending payment record and calls PayOS to get a checkout URL.
func (s *Service) CreatePayOSLink(ctx context.Context, req request.CreatePaymentRequestDTO) (response.PaymentResponseDTO, error) {
	// Start transaction early to prevent race condition (Spam Click).
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. Lock the registration row and check status
	var regStatus regenum.ExamRegistrationStatus
	var amount int64
	if err := tx.QueryRowContext(ctx,
		`SELECT er.exam_registration_status, es.exam_session_fee
		 FROM exam_registrations er
		 JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		 JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		 WHERE er.exam_registration_id = $1
		   AND er.exam_registration_is_deleted = FALSE
		 FOR UPDATE`, // <-- LOCK HERE
		req.ExamRegistrationID,
	).Scan(&regStatus, &amount); errors.Is(err, sql.ErrNoRows) {
		return response.PaymentResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: check reg: %w", err)
	}
	if regStatus == regenum.RegStatusCancelled {
		return response.PaymentResponseDTO{}, errors.New("Không thể thanh toán cho phiếu đăng ký đã hủy")
	}
	if regStatus == regenum.RegStatusConfirmed {
		return response.PaymentResponseDTO{}, errors.New("Phiếu đăng ký đã được xác nhận thanh toán")
	}

	// 2. Check for existing pending payment (now safe because of the lock above)
	var existingID int64
	var existingRef *string
	err = tx.QueryRowContext(ctx,
		`SELECT payment_id, payment_transaction_ref FROM payments
		 WHERE payment_exam_registration_id = $1
		   AND payment_status = 'pending'
		   AND payment_is_deleted = FALSE
		 ORDER BY payment_created_at DESC LIMIT 1`,
		req.ExamRegistrationID,
	).Scan(&existingID, &existingRef)
	if err == nil {
		return response.PaymentResponseDTO{}, errors.New("Đã tồn tại giao dịch chờ thanh toán cho phiếu đăng ký này")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: check existing: %w", err)
	}

	// 3. Insert a pending payment row.
	var e PaymentEntity
	gateway := "payos"
	content := fmt.Sprintf("HSK-%d", req.ExamRegistrationID)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO payments
		       (payment_exam_registration_id, payment_amount, payment_method, payment_gateway, payment_content, payment_status)
		VALUES ($1, $2, 'bank_transfer', 'payos', $3, 'pending')
		RETURNING `+paymentCols,
		req.ExamRegistrationID, amount, content,
	).Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID, &e.PaymentContent,
		&e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
	if err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: insert: %w", err)
	}

	// 4. Store the transaction_ref (orderCode as string) back.
	// Add timestamp offset to payment ID to prevent PayOS "Order Already Exists" error after DB resets.
	orderCode := int64(time.Now().Unix())*10000 + e.PaymentID
	orderCodeStr := fmt.Sprintf("%d", orderCode)
	if _, err := tx.ExecContext(ctx,
		`UPDATE payments SET payment_transaction_ref = $1 WHERE payment_id = $2`,
		orderCodeStr, e.PaymentID,
	); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: update ref: %w", err)
	}
	e.PaymentTransactionRef = &orderCodeStr
	e.PaymentGateway = &gateway

	// 5. Audit log.
	newVal, _ := json.Marshal(map[string]any{
		"registration_id": req.ExamRegistrationID,
		"amount":          amount,
		"gateway":         "payos",
		"order_code":      orderCode,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		Action:      auditenum.AuditActionPayment.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    e.PaymentID,
		NewValue:    newVal,
	})

	// 6. GIẢI PHÓNG CONNECTION DB TRƯỚC KHI GỌI MẠNG
	if err := tx.Commit(); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: commit: %w", err)
	}

	// 7. Gọi PayOS API để lấy Link (Mất 200-300ms, KHÔNG chiếm dụng DB Connection)
	checkoutURL, err := s.callPayOSCreateLink(ctx, orderCode, amount, content, req.ReturnURL, req.CancelURL)
	if err != nil {
		// Rollback logic (đã thoát tx nên phải dùng lệnh update riêng)
		_, _ = s.db.ExecContext(context.Background(), "UPDATE payments SET payment_status = 'failed' WHERE payment_id = $1", e.PaymentID)
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: payos create link: %w", err)
	}

	dto := ToPaymentResponseDTO(e)
	dto.CheckoutURL = &checkoutURL
	return dto, nil
}

// ─── UC3: PayOS Webhook via stored procedure (Idempotent) ───────────────────

// ProcessWebhook handles the PayOS webhook callback via fn_process_payos_webhook.
// The stored procedure handles: lock → idempotency guard → update → confirm reg → book seat.
// All atomic in a single PL/pgSQL call. No raw SQL needed in Go.
func (s *Service) ProcessWebhook(ctx context.Context, payload request.PayOSWebhookPayload) error {
	// 1. Verify HMAC-SHA256 signature (Go-side, before touching DB).
	if err := s.verifyPayOSSignature(payload); err != nil {
		return fmt.Errorf("signature: %w", err)
	}

	// 2. Call the stored procedure — it does everything atomically:
	//    - Logs raw webhook to payment_webhook_logs
	//    - SELECT FOR UPDATE on payment row
	//    - Idempotency guard (already processed → returns -1)
	//    - Update payment status + gateway_trans_id
	//    - Confirm registration + book seat (if success)
	rawJSON, _ := json.Marshal(payload)
	isSuccess := payload.Code == "00" && payload.Success

	var resultID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT fn_process_payos_webhook($1, $2, $3, $4)",
		payload.Data.OrderCode,
		payload.Data.Reference,
		isSuccess,
		rawJSON,
	).Scan(&resultID)
	if err != nil {
		log.Printf("[payment] webhook: fn_process_payos_webhook failed for order_code=%d: %v", payload.Data.OrderCode, err)
		return fmt.Errorf("payment: process webhook: %w", err)
	}

	// resultID semantics:
	//   0  = payment not found (silently ignored)
	//  -1  = already processed (idempotency guard triggered)
	//  >0  = payment_id that was updated
	switch {
	case resultID == 0:
		log.Printf("[payment] webhook: unknown order_code=%d, ignoring", payload.Data.OrderCode)
	case resultID == -1:
		log.Printf("[payment] webhook: order_code=%d already processed, skipping", payload.Data.OrderCode)
	case resultID == -2:
		log.Printf("[payment] webhook: amount mismatch for order_code=%d (possible underpayment/overpayment)", payload.Data.OrderCode)
		return fmt.Errorf("amount mismatch")
	default:
		// Audit log (outside the procedure, non-fatal).
		action := auditenum.AuditActionPayment
		status := "failed"
		if isSuccess {
			status = "success"
		}
		newVal, _ := json.Marshal(map[string]any{"status": status, "gateway_ref": payload.Data.Reference})
		oldVal, _ := json.Marshal(map[string]any{"status": "pending"})
		_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
			Action:      action.String(),
			EntityTable: auditenum.AuditEntityPayment.String(),
			EntityID:    resultID,
			OldValue:    oldVal,
			NewValue:    newVal,
		})
		log.Printf("[payment] webhook: payment_id=%d updated to %s", resultID, status)
		log.Printf("[payment] webhook: payment_id=%d updated to %s", resultID, status)

		// Broadcast real-time event if successful
		if isSuccess && s.hub != nil {
			var regID, seatID int64
			err := s.db.QueryRowContext(ctx, `
				SELECT p.payment_exam_registration_id, r.exam_registration_exam_seat_id
				FROM payments p
				JOIN exam_registrations r ON r.exam_registration_id = p.payment_exam_registration_id
				WHERE p.payment_id = $1
			`, resultID).Scan(&regID, &seatID)
			if err == nil {
				s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
					"registration_id": regID,
					"seat_id":         seatID,
					"status":          "confirmed",
				})
			}
		}
	}

	return nil
}

// ─── UC2: Candidate payment history ─────────────────────────────────────────

// GetPaymentHistory returns a candidate's payment DTOs.
func (s *Service) GetPaymentHistory(ctx context.Context, candidateID int64) ([]response.PaymentResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+paymentCols+`
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		WHERE er.exam_registration_candidate_id = $1 AND p.payment_is_deleted = FALSE
		ORDER BY p.payment_created_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("payment: history: %w", err)
	}
	defer rows.Close()
	var list []response.PaymentResponseDTO
	for rows.Next() {
		var e PaymentEntity
		if err := scanPayment(rows, &e); err != nil {
			return nil, fmt.Errorf("payment: scan: %w", err)
		}
		list = append(list, ToPaymentResponseDTO(e))
	}
	return list, rows.Err()
}

// ─── UC4: Admin list all payments (paginated) ───────────────────────────────

// AdminListPayments returns all payments for the admin panel with pagination.
func (s *Service) AdminListPayments(ctx context.Context, req request.AdminListPaymentsRequestDTO) ([]response.AdminPaymentResponseDTO, *utils.Meta, error) {
	if err := validateDateFilters(req); err != nil {
		return nil, nil, err
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Build WHERE clause dynamically.
	where := "p.payment_is_deleted = FALSE"
	args := []any{}
	argIdx := 1

	if req.Status != "" {
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "cancelled" {
			where += " AND p.payment_status IN ('cancelled', 'failed', 'expired')"
		} else if !payenum.PaymentStatus(status).Valid() {
			return nil, nil, errors.New("Trạng thái thanh toán không hợp lệ")
		} else {
			where += fmt.Sprintf(" AND p.payment_status = $%d", argIdx)
			args = append(args, status)
			argIdx++
		}
	}
	if req.From != "" {
		where += fmt.Sprintf(" AND p.payment_created_at >= $%d::date", argIdx)
		args = append(args, req.From)
		argIdx++
	}
	if req.To != "" {
		where += fmt.Sprintf(" AND p.payment_created_at < ($%d::date + INTERVAL '1 day')", argIdx)
		args = append(args, req.To)
		argIdx++
	}
	if req.OrderCode != "" {
		where += fmt.Sprintf(" AND p.payment_transaction_ref = $%d", argIdx)
		args = append(args, req.OrderCode)
		argIdx++
	}
	if req.TransactionID != "" {
		where += fmt.Sprintf(" AND p.payment_gateway_trans_id = $%d", argIdx)
		args = append(args, req.TransactionID)
		argIdx++
	}

	// Count total.
	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM payments p WHERE %s", where)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("payment: admin count: %w", err)
	}

	// Fetch page.
	selectQuery := fmt.Sprintf(`SELECT %s FROM payments p WHERE %s ORDER BY p.payment_created_at DESC LIMIT $%d OFFSET $%d`,
		adminPaymentCols, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("payment: admin list: %w", err)
	}
	defer rows.Close()

	var list []response.AdminPaymentResponseDTO
	for rows.Next() {
		var e PaymentEntity
		if err := scanAdminPayment(rows, &e); err != nil {
			return nil, nil, fmt.Errorf("payment: admin scan: %w", err)
		}
		list = append(list, ToAdminPaymentResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("payment: admin rows: %w", err)
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
	return list, meta, nil
}

// ExportPaymentsCSV returns an accounting-friendly CSV export using the same
// filters as the admin list endpoint.
func (s *Service) ExportPaymentsCSV(ctx context.Context, req request.AdminListPaymentsRequestDTO) ([]byte, error) {
	if err := validateDateFilters(req); err != nil {
		return nil, err
	}
	where := "p.payment_is_deleted = FALSE"
	args := []any{}
	argIdx := 1
	if req.Status != "" {
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "cancelled" {
			where += " AND p.payment_status IN ('cancelled', 'failed', 'expired')"
		} else if !payenum.PaymentStatus(status).Valid() {
			return nil, errors.New("Trạng thái thanh toán không hợp lệ")
		} else {
			where += fmt.Sprintf(" AND p.payment_status = $%d", argIdx)
			args = append(args, status)
			argIdx++
		}
	}
	if req.From != "" {
		where += fmt.Sprintf(" AND p.payment_created_at >= $%d::date", argIdx)
		args = append(args, req.From)
		argIdx++
	}
	if req.To != "" {
		where += fmt.Sprintf(" AND p.payment_created_at < ($%d::date + INTERVAL '1 day')", argIdx)
		args = append(args, req.To)
		argIdx++
	}
	if req.OrderCode != "" {
		where += fmt.Sprintf(" AND p.payment_transaction_ref = $%d", argIdx)
		args = append(args, req.OrderCode)
		argIdx++
	}
	if req.TransactionID != "" {
		where += fmt.Sprintf(" AND p.payment_gateway_trans_id = $%d", argIdx)
		args = append(args, req.TransactionID)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT p.payment_transaction_ref, c.candidate_full_name, p.payment_amount,
		       p.payment_amount_received, p.payment_created_at, p.payment_status
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		  AND er.exam_registration_is_deleted = FALSE
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		  AND c.candidate_is_deleted = FALSE
		WHERE %s
		ORDER BY p.payment_created_at DESC`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("payment: export query: %w", err)
	}
	defer rows.Close()

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Mã đơn hàng", "Tên thí sinh", "Số tiền yêu cầu", "Số tiền thực nhận", "Thời gian", "Trạng thái"}); err != nil {
		return nil, fmt.Errorf("payment: export header: %w", err)
	}
	for rows.Next() {
		var orderCode, name, status string
		var amount int64
		var received sql.NullInt64
		var created time.Time
		if err := rows.Scan(&orderCode, &name, &amount, &received, &created, &status); err != nil {
			return nil, fmt.Errorf("payment: export scan: %w", err)
		}
		receivedValue := ""
		if received.Valid {
			receivedValue = strconv.FormatInt(received.Int64, 10)
		}
		if err := w.Write([]string{orderCode, name, strconv.FormatInt(amount, 10), receivedValue, created.Format(time.RFC3339), status}); err != nil {
			return nil, fmt.Errorf("payment: export row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payment: export rows: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("payment: export flush: %w", err)
	}
	return buf.Bytes(), nil
}

func validateDateFilters(req request.AdminListPaymentsRequestDTO) error {
	for _, value := range []string{req.From, req.To} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return errors.New("Ngày lọc phải có định dạng YYYY-MM-DD")
		}
	}
	if req.From != "" && req.To != "" && req.From > req.To {
		return errors.New("Khoảng ngày lọc không hợp lệ")
	}
	return nil
}

// AdminConfirmPaymentAs confirms a pending payment while locking the payment,
// registration, and seat in one transaction. actorID is written to audit_logs.
func (s *Service) AdminConfirmPaymentAs(ctx context.Context, paymentID, actorID int64) (response.AdminPaymentResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var regID, amount int64
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_exam_registration_id, payment_amount, payment_status
		FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, paymentID).Scan(&regID, &amount, &status); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm lock payment: %w", err)
	}
	if status != payenum.PayStatusPending.String() {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch không ở trạng thái chờ thanh toán")
	}

	var seatID int64
	var regStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, regID).Scan(&seatID, &regStatus); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm lock registration: %w", err)
	}
	if regStatus != regenum.RegStatusPendingPayment.String() {
		return response.AdminPaymentResponseDTO{}, errors.New("Phiếu đăng ký không còn ở trạng thái chờ thanh toán")
	}

	var seatStatus string
	var heldUntil *time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_seat_status, exam_seat_held_until
		FROM exam_seats
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE
		FOR UPDATE`, seatID).Scan(&seatStatus, &heldUntil); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không còn ghế trống cho phiếu đăng ký")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm lock seat: %w", err)
	}
	if seatStatus == "booked" || (seatStatus == "held" && heldUntil != nil && heldUntil.Before(time.Now())) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không còn ghế trống cho phiếu đăng ký")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments SET payment_status = 'success', payment_paid_at = NOW(),
		       payment_amount_received = COALESCE(payment_amount_received, payment_amount)
		WHERE payment_id = $1`, paymentID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm payment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations SET exam_registration_status = 'confirmed'
		WHERE exam_registration_id = $1`, regID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm registration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL
		WHERE exam_seat_id = $1`, seatID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm seat: %w", err)
	}
	oldVal, _ := json.Marshal(map[string]any{"status": "pending"})
	newVal, _ := json.Marshal(map[string]any{"status": "success", "action": "admin_manual_confirm"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionApprove.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID,
		OldValue: oldVal, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm commit: %w", err)
	}
	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after confirm: %w", err)
	}
	updated.PaymentAmountReceived = &amount
	return ToAdminPaymentResponseDTO(updated), nil
}

// AdminMarkRefund marks a paid or cancelled payment for manual bank refund.
func (s *Service) AdminMarkRefund(ctx context.Context, paymentID, actorID int64) (response.AdminPaymentResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: refund begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`, paymentID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: refund lock: %w", err)
	}
	if status != "success" && status != "cancelled" && status != "failed" && status != "expired" {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch chưa đủ điều kiện đánh dấu hoàn tiền")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET payment_status = 'refunded' WHERE payment_id = $1`, paymentID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: mark refund: %w", err)
	}
	oldVal, _ := json.Marshal(map[string]any{"status": status})
	newVal, _ := json.Marshal(map[string]any{"status": "refunded", "action": "mark_refund"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionRefund.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID,
		OldValue: oldVal, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: refund commit: %w", err)
	}
	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after refund: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// AdminCancelPaymentAs cancels a payment and releases its registration seat
// atomically. It is used by the admin route and expiry workflow.
func (s *Service) AdminCancelPaymentAs(ctx context.Context, paymentID, actorID int64) (response.AdminPaymentResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	var regID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status, payment_exam_registration_id
		FROM payments WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`,
		paymentID).Scan(&status, &regID); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel lock: %w", err)
	}
	var newStatus string
	switch status {
	case "pending":
		newStatus = "cancelled"
	case "success":
		newStatus = "refunded"
	default:
		return response.AdminPaymentResponseDTO{}, errors.New("Không thể hủy giao dịch ở trạng thái này")
	}
	paymentUpdate, err := tx.ExecContext(ctx, `
		UPDATE payments SET payment_status = $1 WHERE payment_id = $2`, newStatus, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel payment: %w", err)
	}
	if affected, err := paymentUpdate.RowsAffected(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel payment affected rows: %w", err)
	} else if affected != 1 {
		return response.AdminPaymentResponseDTO{}, errors.New("Không thể cập nhật giao dịch")
	}
	var seatID int64
	var regStatus string
	var seatStatus string
	var examDate time.Time
	var examPassed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_exam_seat_id, er.exam_registration_status,
		       es.exam_session_date, es.exam_session_date <= CURRENT_DATE,
		       seat.exam_seat_status
		FROM exam_registrations er
		JOIN exam_sessions es
		  ON es.exam_session_id = er.exam_registration_exam_session_id
		 AND es.exam_session_is_deleted = FALSE
		JOIN exam_seats seat
		  ON seat.exam_seat_id = er.exam_registration_exam_seat_id
		 AND seat.exam_seat_is_deleted = FALSE
		WHERE er.exam_registration_id = $1
		  AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er, es, seat`, regID).
		Scan(&seatID, &regStatus, &examDate, &examPassed, &seatStatus); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel registration lock: %w", err)
	}
	if examPassed {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf(
			"Không thể hủy giao dịch sau ngày thi (%s); giao dịch và ghế được giữ nguyên",
			examDate.Format("2006-01-02"),
		)
	}
	if regStatus == "cancelled" {
		return response.AdminPaymentResponseDTO{}, errors.New("Phiếu đăng ký đã bị hủy từ trước")
	}
	if regStatus != "pending_payment" && regStatus != "confirmed" {
		return response.AdminPaymentResponseDTO{}, errors.New("Phiếu đăng ký không ở trạng thái có thể hủy")
	}
	registrationUpdate, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations SET exam_registration_status = 'cancelled'
		WHERE exam_registration_id = $1
		  AND exam_registration_is_deleted = FALSE`, regID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel registration: %w", err)
	}
	if affected, err := registrationUpdate.RowsAffected(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel registration affected rows: %w", err)
	} else if affected != 1 {
		return response.AdminPaymentResponseDTO{}, errors.New("Không thể cập nhật phiếu đăng ký")
	}
	seatReleased := false
	if seatStatus == "held" || seatStatus == "booked" {
		result, err := tx.ExecContext(ctx, `
			UPDATE exam_seats
			SET exam_seat_status = 'available', exam_seat_held_until = NULL
			WHERE exam_seat_id = $1
			  AND exam_seat_status IN ('held', 'booked')
			  AND exam_seat_is_deleted = FALSE`, seatID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel seat: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel seat affected rows: %w", err)
		}
		if affected != 1 {
			return response.AdminPaymentResponseDTO{}, errors.New("Không thể trả lại ghế")
		}
		seatReleased = true
	}
	oldVal, _ := json.Marshal(map[string]any{"status": status})
	action := auditenum.AuditActionReject
	actionName := "admin_cancel"
	if status == "success" {
		action = auditenum.AuditActionCancelSuccessPayment
		actionName = action.String()
	}
	newVal, _ := json.Marshal(map[string]any{
		"status":          newStatus,
		"action":          actionName,
		"transaction_id":  paymentID,
		"registration_id": regID,
		"seat_id":         seatID,
		"seat_released":   seatReleased,
	})
	if err := s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: action.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID,
		OldValue: oldVal, NewValue: newVal,
	}); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel commit: %w", err)
	}
	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after cancel: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// SyncPayment checks PayOS and applies a PAID result atomically.
func (s *Service) SyncPayment(ctx context.Context, paymentID, actorID int64) (response.AdminPaymentResponseDTO, error) {
	var orderCode string
	var expected int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT payment_transaction_ref, payment_amount
		FROM payments WHERE payment_id = $1 AND payment_is_deleted = FALSE`, paymentID).Scan(&orderCode, &expected); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync load: %w", err)
	}
	code, err := strconv.ParseInt(orderCode, 10, 64)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch không có mã đơn PayOS hợp lệ")
	}
	state, received, gatewayRef, err := s.callPayOSStatus(ctx, code)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync payos: %w", err)
	}
	if strings.ToUpper(state) != "PAID" {
		return response.AdminPaymentResponseDTO{}, errors.New("PayOS chưa ghi nhận giao dịch đã thanh toán")
	}
	if os.Getenv("MOCK_PAYOS") == "true" && received == 0 {
		received = expected
	}
	if received != expected {
		return response.AdminPaymentResponseDTO{}, errors.New("Số tiền thanh toán không khớp")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	var current string
	var regID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status, payment_exam_registration_id
		FROM payments WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`, paymentID).Scan(&current, &regID); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync lock: %w", err)
	}
	if current != "pending" {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync reload: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments SET payment_status = 'success', payment_amount_received = $1,
		       payment_gateway_trans_id = NULLIF($2, ''), payment_paid_at = NOW()
		WHERE payment_id = $3`, received, gatewayRef, paymentID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync payment: %w", err)
	}
	var seatID int64
	var regStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status
		FROM exam_registrations WHERE exam_registration_id = $1
		  AND exam_registration_is_deleted = FALSE FOR UPDATE`, regID).Scan(&seatID, &regStatus); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync registration: %w", err)
	}
	if regStatus != regenum.RegStatusPendingPayment.String() {
		return response.AdminPaymentResponseDTO{}, errors.New("Phiếu đăng ký không còn ở trạng thái chờ thanh toán")
	}
	var seatStatus string
	var heldUntil *time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_seat_status, exam_seat_held_until FROM exam_seats
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE FOR UPDATE`, seatID).Scan(&seatStatus, &heldUntil); err != nil {
		return response.AdminPaymentResponseDTO{}, errors.New("Không còn ghế trống cho phiếu đăng ký")
	}
	if seatStatus == "booked" || (seatStatus == "held" && heldUntil != nil && heldUntil.Before(time.Now())) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không còn ghế trống cho phiếu đăng ký")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE exam_registrations SET exam_registration_status = 'confirmed' WHERE exam_registration_id = $1`, regID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync confirm registration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL WHERE exam_seat_id = $1`, seatID); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync book seat: %w", err)
	}
	newVal, _ := json.Marshal(map[string]any{"status": "success", "action": "payos_sync"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionPayment.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync commit: %w", err)
	}
	updated, err := s.fetchPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync reload: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

func (s *Service) callPayOSStatus(ctx context.Context, orderCode int64) (status string, amount int64, reference string, err error) {
	if os.Getenv("MOCK_PAYOS") == "true" {
		return "PAID", 0, "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api-merchant.payos.vn/v2/payment-requests/%d", orderCode), nil)
	if err != nil {
		return "", 0, "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("x-client-id", s.payOS.ClientID)
	req.Header.Set("x-api-key", s.payOS.ApiKey)
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Code string `json:"code"`
		Desc string `json:"desc"`
		Data struct {
			Status    string `json:"status"`
			Amount    int64  `json:"amount"`
			Reference string `json:"reference"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, "", fmt.Errorf("decode response: %w", err)
	}
	if result.Code != "00" {
		return "", 0, "", fmt.Errorf("payos error: code=%s desc=%s", result.Code, result.Desc)
	}
	return result.Data.Status, result.Data.Amount, result.Data.Reference, nil
}

// ─── UC5: Admin manual confirm via stored procedure ─────────────────────────

// AdminConfirmPayment calls fn_admin_confirm_payment (lock → update → confirm reg → book seat).
func (s *Service) AdminConfirmPayment(ctx context.Context, paymentID int64) (response.AdminPaymentResponseDTO, error) {
	var resultID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT fn_admin_confirm_payment($1)", paymentID,
	).Scan(&resultID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "PAYMENT_NOT_FOUND"):
			return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
		case strings.Contains(err.Error(), "PAYMENT_NOT_PENDING"):
			return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch không ở trạng thái chờ thanh toán")
		default:
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: admin confirm: %w", err)
		}
	}

	// Audit log (outside procedure, non-fatal).
	oldVal, _ := json.Marshal(map[string]any{"status": "pending"})
	newVal, _ := json.Marshal(map[string]any{"status": "success", "action": "admin_manual_confirm"})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		Action:      auditenum.AuditActionApprove.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    resultID,
		OldValue:    oldVal,
		NewValue:    newVal,
	})

	// Reload the updated row to return fresh DTO.
	updated, err := s.fetchPaymentByID(ctx, resultID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after confirm: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// ─── UC6: Admin cancel payment via stored procedure ─────────────────────────

// AdminCancelPayment calls fn_admin_cancel_payment (lock → determine target status → update).
func (s *Service) AdminCancelPayment(ctx context.Context, paymentID int64) (response.AdminPaymentResponseDTO, error) {
	var newStatus string
	err := s.db.QueryRowContext(ctx,
		"SELECT fn_admin_cancel_payment($1)", paymentID,
	).Scan(&newStatus)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "PAYMENT_NOT_FOUND"):
			return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
		case strings.Contains(err.Error(), "PAYMENT_NOT_CANCELLABLE"):
			return response.AdminPaymentResponseDTO{}, errors.New("Không thể hủy giao dịch ở trạng thái này")
		default:
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: admin cancel: %w", err)
		}
	}

	// Audit log (outside procedure, non-fatal).
	action := auditenum.AuditActionRefund
	if newStatus == "failed" {
		action = auditenum.AuditActionReject
	}
	oldVal, _ := json.Marshal(map[string]any{"action": "admin_cancel"})
	newVal, _ := json.Marshal(map[string]any{"status": newStatus})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		Action:      action.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    paymentID,
		OldValue:    oldVal,
		NewValue:    newVal,
	})

	// Reload the updated row to return fresh DTO.
	updated, err := s.fetchPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after cancel: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// ─── PayOS HTTP Client ──────────────────────────────────────────────────────

// callPayOSCreateLink calls the PayOS REST API to create a payment link.
func (s *Service) callPayOSCreateLink(ctx context.Context, orderCode int64, amount int64, description string, returnURL, cancelURL string) (string, error) {
	if os.Getenv("MOCK_PAYOS") == "true" {
		return "https://pay.payos.vn/web/mock/" + fmt.Sprintf("%d", orderCode), nil
	}
	type item struct {
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
		Price    int64  `json:"price"`
	}
	body := map[string]any{
		"orderCode":   orderCode,
		"amount":      amount,
		"description": description,
		"items": []item{
			{Name: "Lệ phí thi HSK", Quantity: 1, Price: amount},
		},
		"cancelUrl": cancelURL,
		"returnUrl": returnURL,
	}

	// Generate signature: sort keys alphabetically, join as key=value&, HMAC-SHA256.
	sigData := fmt.Sprintf("amount=%d&cancelUrl=%s&description=%s&orderCode=%d&returnUrl=%s",
		amount, cancelURL, description, orderCode, returnURL)
	mac := hmac.New(sha256.New, []byte(s.payOS.ChecksumKey))
	mac.Write([]byte(sigData))
	signature := hex.EncodeToString(mac.Sum(nil))
	body["signature"] = signature

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api-merchant.payos.vn/v2/payment-requests", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", s.payOS.ClientID)
	req.Header.Set("x-api-key", s.payOS.ApiKey)

	resp, err := s.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code string `json:"code"`
		Desc string `json:"desc"`
		Data struct {
			CheckoutURL string `json:"checkoutUrl"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Code != "00" {
		return "", fmt.Errorf("payos error: code=%s desc=%s", result.Code, result.Desc)
	}
	if result.Data.CheckoutURL == "" {
		return "", errors.New("payos returned empty checkoutUrl")
	}
	return result.Data.CheckoutURL, nil
}

// ─── PayOS Signature Verification ───────────────────────────────────────────

// verifyPayOSSignature verifies the HMAC-SHA256 signature from PayOS.
func (s *Service) verifyPayOSSignature(payload request.PayOSWebhookPayload) error {
	if s.payOS.ChecksumKey == "" {
		log.Println("[payment] WARNING: PAY_OS_CHECKSUM_KEY is empty, skipping signature verification")
		return nil
	}

	// Build the data string: sort keys of Data alphabetically, join as key=value&.
	dataMap := map[string]string{
		"orderCode":              fmt.Sprintf("%d", payload.Data.OrderCode),
		"amount":                 fmt.Sprintf("%d", payload.Data.Amount),
		"description":            payload.Data.Description,
		"accountNumber":          payload.Data.AccountNumber,
		"reference":              payload.Data.Reference,
		"transactionDateTime":    payload.Data.TransactionDateTime,
		"currency":               payload.Data.Currency,
		"paymentLinkId":          payload.Data.PaymentLinkID,
		"code":                   payload.Data.Code,
		"desc":                   payload.Data.Desc,
		"counterAccountBankId":   payload.Data.CounterAccountBankID,
		"counterAccountBankName": payload.Data.CounterAccountBankName,
		"counterAccountName":     payload.Data.CounterAccountName,
		"counterAccountNumber":   payload.Data.CounterAccountNumber,
		"virtualAccountName":     payload.Data.VirtualAccountName,
		"virtualAccountNumber":   payload.Data.VirtualAccountNumber,
	}

	keys := make([]string, 0, len(dataMap))
	for k := range dataMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, dataMap[k]))
	}
	sigData := strings.Join(pairs, "&")

	mac := hmac.New(sha256.New, []byte(s.payOS.ChecksumKey))
	mac.Write([]byte(sigData))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(payload.Signature)) {
		return errors.New("invalid signature")
	}
	return nil
}
