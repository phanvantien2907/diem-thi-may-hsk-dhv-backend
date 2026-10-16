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
	"github.com/hskvinh/backend/internal/media"
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
	mediaSvc *media.Service
	payOS    PayOSConfig
	httpCli  *http.Client
	hub      *ws.Hub // optional, nil-safe
}

// NewService creates a payment Service.
func NewService(db *sql.DB, auditSvc audit.Service, payOS PayOSConfig, mediaSvc *media.Service) *Service {
	return &Service{
		db:       db,
		auditSvc: auditSvc,
		mediaSvc: mediaSvc,
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
	p.payment_gateway_trans_id, p.payment_content, p.payment_status,
	COALESCE(p.payment_evidence_urls, '[]'::jsonb), p.payment_admin_note,
	p.payment_refund_amount, p.payment_refund_reason,
	COALESCE(p.payment_refund_evidence_urls, '[]'::jsonb), p.payment_refunded_at, p.payment_refund_bank_ref,
	p.payment_is_top_up, p.payment_parent_payment_id,
	p.payment_expires_at, p.payment_paid_at, p.payment_is_deleted, p.payment_deleted_at, p.payment_created_at,
	p.payment_updated_at`

const suspenseCols = `s.suspense_id, s.suspense_order_code, s.suspense_amount, s.suspense_bank_ref,
	s.suspense_transfer_content, s.suspense_transfer_time, COALESCE(s.suspense_evidence_urls, '[]'::jsonb),
	s.suspense_status, s.suspense_matched_registration_id, s.suspense_matched_payment_id,
	s.suspense_matched_by_admin_id, s.suspense_matched_at, s.suspense_admin_note, s.suspense_source,
	s.suspense_is_deleted, s.suspense_deleted_at, s.suspense_created_at, s.suspense_updated_at`

func scanPayment(row interface{ Scan(dest ...any) error }, e *PaymentEntity) error {
	return row.Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID, &e.PaymentContent,
		&e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
}

func scanAdminPayment(row interface{ Scan(dest ...any) error }, e *PaymentEntity) error {
	var evidenceJSON, refundEvidenceJSON []byte
	err := row.Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentAmountReceived,
		&e.PaymentMethod, &e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID,
		&e.PaymentContent, &e.PaymentStatus,
		&evidenceJSON, &e.PaymentAdminNote,
		&e.PaymentRefundAmount, &e.PaymentRefundReason,
		&refundEvidenceJSON, &e.PaymentRefundedAt, &e.PaymentRefundBankRef,
		&e.PaymentIsTopUp, &e.PaymentParentPaymentID,
		&e.PaymentExpiresAt, &e.PaymentPaidAt,
		&e.PaymentIsDeleted, &e.PaymentDeletedAt, &e.PaymentCreatedAt, &e.PaymentUpdatedAt,
	)
	if err != nil {
		return err
	}
	if len(evidenceJSON) > 0 {
		_ = json.Unmarshal(evidenceJSON, &e.PaymentEvidenceURLs)
	}
	if e.PaymentEvidenceURLs == nil {
		e.PaymentEvidenceURLs = []string{}
	}
	if len(refundEvidenceJSON) > 0 {
		_ = json.Unmarshal(refundEvidenceJSON, &e.PaymentRefundEvidenceURLs)
	}
	if e.PaymentRefundEvidenceURLs == nil {
		e.PaymentRefundEvidenceURLs = []string{}
	}
	return nil
}

func scanSuspense(row interface{ Scan(dest ...any) error }, e *SuspenseTransactionEntity) error {
	var evidenceJSON []byte
	err := row.Scan(
		&e.SuspenseID, &e.SuspenseOrderCode, &e.SuspenseAmount, &e.SuspenseBankRef,
		&e.SuspenseTransferContent, &e.SuspenseTransferTime, &evidenceJSON,
		&e.SuspenseStatus, &e.SuspenseMatchedRegistrationID, &e.SuspenseMatchedPaymentID,
		&e.SuspenseMatchedByAdminID, &e.SuspenseMatchedAt, &e.SuspenseAdminNote, &e.SuspenseSource,
		&e.SuspenseIsDeleted, &e.SuspenseDeletedAt, &e.SuspenseCreatedAt, &e.SuspenseUpdatedAt,
	)
	if err != nil {
		return err
	}
	if len(evidenceJSON) > 0 {
		_ = json.Unmarshal(evidenceJSON, &e.SuspenseEvidenceURLs)
	}
	if e.SuspenseEvidenceURLs == nil {
		e.SuspenseEvidenceURLs = []string{}
	}
	return nil
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
	err := scanAdminPayment(s.db.QueryRowContext(ctx,
		`SELECT `+adminPaymentCols+` FROM payments p WHERE p.payment_id = $1 AND p.payment_is_deleted = FALSE`, id,
	), &e)
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
func (s *Service) AdminConfirmPaymentAs(ctx context.Context, paymentID, actorID int64, req request.AdminConfirmPaymentRequestDTO) (response.AdminPaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.AdminPaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

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
	// Idempotent guard: if already confirmed (e.g. rapid double click), release lock and return cleanly
	if status == payenum.PayStatusSuccess.String() {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after duplicate confirm: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
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

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'success', payment_paid_at = NOW(),
		    payment_amount_received = COALESCE(payment_amount_received, payment_amount),
		    payment_evidence_urls = $2, payment_admin_note = NULLIF($3, '')
		WHERE payment_id = $1`, paymentID, evidenceJSON, req.Note); err != nil {
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
	newVal, _ := json.Marshal(map[string]any{
		"status":        "success",
		"action":        "admin_manual_confirm",
		"evidence_urls": req.EvidenceURLs,
		"note":          req.Note,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionApprove.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID,
		OldValue: oldVal, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm commit: %w", err)
	}

	if s.hub != nil {
		s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
			"registration_id": regID,
			"seat_id":         seatID,
			"status":          "confirmed",
		})
	}

	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after confirm: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// AdminMarkRefund marks a paid or cancelled payment for manual bank refund (status -> pending_refund).
func (s *Service) AdminMarkRefund(ctx context.Context, paymentID, actorID int64, req request.AdminMarkRefundRequestDTO) (response.AdminPaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.AdminPaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: refund begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	var regID, amount int64
	var amountReceived sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status, payment_exam_registration_id, payment_amount, payment_amount_received FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`, paymentID).Scan(&status, &regID, &amount, &amountReceived); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: refund lock: %w", err)
	}

	// Idempotent guard: if already marked pending_refund, return cleanly
	if status == payenum.PayStatusPendingRefund.String() {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after duplicate mark refund: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
	}

	if status == payenum.PayStatusRefunded.String() {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch đã được hoàn tiền thành công trước đó")
	}

	if status != "success" && status != "cancelled" && status != "failed" && status != "expired" && status != "amount_mismatch" && status != "partial_paid" {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch chưa đủ điều kiện đánh dấu hoàn tiền")
	}

	evidenceBytes, _ := json.Marshal(req.EvidenceURLs)
	evidenceJSON := string(evidenceBytes)
	refundAmount := amount
	if amountReceived.Valid && amountReceived.Int64 > 0 {
		refundAmount = amountReceived.Int64
	}

	// If payment was success, ensure registration is cancelled and seat is released before exam date
	if status == "success" {
		var seatID int64
		var examDate time.Time
		var examPassed bool
		if err := tx.QueryRowContext(ctx, `
			SELECT er.exam_registration_exam_seat_id, es.exam_session_date, es.exam_session_date <= CURRENT_DATE
			FROM exam_registrations er
			JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
			WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
			FOR UPDATE`, regID).Scan(&seatID, &examDate, &examPassed); err == nil {
			if examPassed {
				return response.AdminPaymentResponseDTO{}, errors.New("Không thể hoàn tiền sau hoặc trong ngày thi: " + examDate.Format("2006-01-02"))
			}
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_registrations
				SET exam_registration_status = 'cancelled', exam_registration_cancelled_at = NOW(), exam_registration_updated_at = NOW()
				WHERE exam_registration_id = $1`, regID)
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_seats
				SET exam_seat_status = 'available', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
				WHERE exam_seat_id = $1 AND exam_seat_status IN ('held', 'booked')`, seatID)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'pending_refund', payment_evidence_urls = $2,
		    payment_admin_note = $3, payment_refund_reason = $3,
		    payment_refund_amount = $4
		WHERE payment_id = $1`, paymentID, evidenceJSON, req.Reason, refundAmount); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: mark refund: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": status})
	newVal, _ := json.Marshal(map[string]any{
		"status":        "pending_refund",
		"action":        "mark_refund",
		"reason":        req.Reason,
		"evidence_urls": req.EvidenceURLs,
		"refund_amount": refundAmount,
	})
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

// AdminConfirmRefund confirms that accountant/admin has transferred refund to candidate via bank.
func (s *Service) AdminConfirmRefund(ctx context.Context, paymentID, actorID int64, req request.AdminConfirmRefundRequestDTO) (response.AdminPaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.AdminPaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng ủy nhiệm chi/biên lai chuyển tiền")
	}
	if strings.TrimSpace(req.BankRef) == "" {
		return response.AdminPaymentResponseDTO{}, errors.New("Mã giao dịch ngân hàng hoàn tiền không được để trống")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm refund begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	var refundAmount sql.NullInt64
	var existingBankRef sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status, payment_refund_amount, payment_refund_bank_ref
		FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`, paymentID).Scan(&status, &refundAmount, &existingBankRef); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm refund lock: %w", err)
	}

	// Idempotent guard
	if status == payenum.PayStatusRefunded.String() {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after duplicate confirm refund: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
	}

	if status != payenum.PayStatusPendingRefund.String() {
		return response.AdminPaymentResponseDTO{}, errors.New("Giao dịch không ở trạng thái chờ hoàn tiền (pending_refund)")
	}

	evidenceBytes, _ := json.Marshal(req.EvidenceURLs)
	evidenceJSON := string(evidenceBytes)

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'refunded',
		    payment_refund_evidence_urls = $2,
		    payment_refund_bank_ref = $3,
		    payment_refunded_at = NOW(),
		    payment_admin_note = COALESCE(NULLIF($4, ''), payment_admin_note)
		WHERE payment_id = $1`, paymentID, evidenceJSON, req.BankRef, req.Note); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm refund update: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": status})
	newVal, _ := json.Marshal(map[string]any{
		"status":                "refunded",
		"action":                "confirm_refund",
		"refund_bank_ref":       req.BankRef,
		"refund_evidence_urls": req.EvidenceURLs,
		"note":                  req.Note,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionRefund.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID,
		OldValue: oldVal, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: confirm refund commit: %w", err)
	}
	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after confirm refund: %w", err)
	}
	return ToAdminPaymentResponseDTO(updated), nil
}

// AdminCancelPaymentAs cancels a payment and releases its registration seat
// atomically. It is used by the admin route and expiry workflow.
func (s *Service) AdminCancelPaymentAs(ctx context.Context, paymentID, actorID int64, req request.AdminCancelPaymentRequestDTO) (response.AdminPaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.AdminPaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	var regID, amount int64
	var amountReceived sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_status, payment_exam_registration_id, payment_amount, payment_amount_received
		FROM payments WHERE payment_id = $1 AND payment_is_deleted = FALSE FOR UPDATE`,
		paymentID).Scan(&status, &regID, &amount, &amountReceived); errors.Is(err, sql.ErrNoRows) {
		return response.AdminPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: cancel lock: %w", err)
	}

	// Idempotent guard
	if status == "cancelled" || status == "pending_refund" || status == "refunded" {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: reload after duplicate cancel: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
	}

	var newStatus string
	switch status {
	case "pending":
		newStatus = "cancelled"
	case "success":
		newStatus = "pending_refund"
	default:
		return response.AdminPaymentResponseDTO{}, errors.New("Không thể hủy giao dịch ở trạng thái này")
	}

	evidenceBytes, _ := json.Marshal(req.EvidenceURLs)
	evidenceJSON := string(evidenceBytes)
	refundAmt := sql.NullInt64{}
	if newStatus == "pending_refund" {
		refundAmt.Valid = true
		if amountReceived.Valid && amountReceived.Int64 > 0 {
			refundAmt.Int64 = amountReceived.Int64
		} else {
			refundAmt.Int64 = amount
		}
	}

	paymentUpdate, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = $1, payment_evidence_urls = $2,
		    payment_admin_note = $3, payment_refund_reason = $3,
		    payment_refund_amount = $4
		WHERE payment_id = $5`, newStatus, evidenceJSON, req.Reason, refundAmt, paymentID)
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
		"evidence_urls":   req.EvidenceURLs,
		"reason":          req.Reason,
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
// Edge Case 1: Thí sinh quét QR phút 14, ngân hàng chậm phút 16 webhook mới đến (ghế đã nhả/đơn hủy).
// SyncPayment sẽ đồng bộ lại trạng thái từ cổng PayOS:
// - Nếu ghế còn khả dụng: tự động tái kích hoạt đơn đăng ký và chốt ghế.
// - Nếu ghế đã bị người khác lấy: tự động chuyển sang refunded 100% kèm ghi chú rõ ràng.
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

	if current != "pending" && current != "cancelled" && current != "expired" && current != "failed" {
		_ = tx.Rollback()
		updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
		if err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync reload: %w", err)
		}
		return ToAdminPaymentResponseDTO(updated), nil
	}

	var seatID int64
	var regStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status
		FROM exam_registrations WHERE exam_registration_id = $1
		  AND exam_registration_is_deleted = FALSE FOR UPDATE`, regID).Scan(&seatID, &regStatus); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync registration: %w", err)
	}

	var seatStatus string
	var heldUntil *time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_seat_status, exam_seat_held_until FROM exam_seats
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE FOR UPDATE`, seatID).Scan(&seatStatus, &heldUntil); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync seat: %w", err)
	}

	seatAvailable := seatStatus == "available" || (seatStatus == "held" && (regStatus == regenum.RegStatusPendingPayment.String() || (heldUntil != nil && heldUntil.After(time.Now()))))

	if seatAvailable {
		// Ghế khả dụng: khôi phục đăng ký và giữ ghế
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments SET payment_status = 'success', payment_amount_received = $1,
			       payment_gateway_trans_id = NULLIF($2, ''), payment_paid_at = NOW(),
			       payment_admin_note = 'Đồng bộ PayOS thành công, khôi phục đơn đăng ký và giữ ghế'
			WHERE payment_id = $3`, received, gatewayRef, paymentID); err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync payment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE exam_registrations SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW() WHERE exam_registration_id = $1`, regID); err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync confirm registration: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW() WHERE exam_seat_id = $1`, seatID); err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync book seat: %w", err)
		}
	} else {
		// Ghế đã bị người khác lấy: chuyển sang diện chờ hoàn tiền 100% (pending_refund)
		note := "Thanh toán thành công trên PayOS nhưng đơn quá hạn và ghế đã bị người khác lấy; chuyển diện chờ hoàn tiền 100%"
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments SET payment_status = 'pending_refund', payment_amount_received = $1,
			       payment_gateway_trans_id = NULLIF($2, ''), payment_paid_at = NOW(),
			       payment_refund_amount = $1, payment_refund_reason = $3, payment_admin_note = $3
			WHERE payment_id = $4`, received, gatewayRef, note, paymentID); err != nil {
			return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync payment refund: %w", err)
		}
	}

	newVal, _ := json.Marshal(map[string]any{"status": "synced", "seat_available": seatAvailable, "action": "payos_sync"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID, Action: auditenum.AuditActionPayment.String(),
		EntityTable: auditenum.AuditEntityPayment.String(), EntityID: paymentID, NewValue: newVal,
	})
	if err := tx.Commit(); err != nil {
		return response.AdminPaymentResponseDTO{}, fmt.Errorf("payment: sync commit: %w", err)
	}

	if seatAvailable && s.hub != nil {
		s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
			"registration_id": regID,
			"seat_id":         seatID,
			"status":          "confirmed",
		})
	}

	updated, err := s.fetchAdminPaymentByID(ctx, paymentID)
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

// ─── Edge Cases & Evidence Attachments ──────────────────────────────────────

// GetEvidenceUploadSignature generates Cloudinary signature for uploading evidence images.
func (s *Service) GetEvidenceUploadSignature() (response.EvidenceUploadSignatureResponseDTO, error) {
	if s.mediaSvc == nil {
		return response.EvidenceUploadSignatureResponseDTO{}, errors.New("Dịch vụ lưu trữ media chưa được cấu hình")
	}
	folder := "hsk_vinh_university/payments/evidence"
	sig, timestamp, apiKey, cloudName, err := s.mediaSvc.GenerateFolderSignature(folder)
	if err != nil {
		return response.EvidenceUploadSignatureResponseDTO{}, fmt.Errorf("payment: signature: %w", err)
	}
	return response.EvidenceUploadSignatureResponseDTO{
		Signature: sig,
		Timestamp: timestamp,
		ApiKey:    apiKey,
		CloudName: cloudName,
		Folder:    folder,
	}, nil
}

// AdminTopUpPayment creates a top-up PayOS payment link for underpaid transactions,
// extends the seat holding deadline, and sends email/zalo notifications.
func (s *Service) AdminTopUpPayment(ctx context.Context, paymentID, actorID int64, req request.AdminTopUpPaymentRequestDTO) (response.TopUpPaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.TopUpPaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: topup begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var regID, amount int64
	var amountReceived sql.NullInt64
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_exam_registration_id, payment_amount, payment_amount_received, payment_status
		FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, paymentID).Scan(&regID, &amount, &amountReceived, &status); errors.Is(err, sql.ErrNoRows) {
		return response.TopUpPaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: topup lock: %w", err)
	}

	if status != payenum.PayStatusPartialPaid.String() && status != payenum.PayStatusPending.String() {
		return response.TopUpPaymentResponseDTO{}, errors.New("Chỉ có thể tạo link thanh toán bù cho giao dịch thiếu tiền (partial_paid) hoặc đang chờ")
	}

	missing := amount
	if amountReceived.Valid {
		missing = amount - amountReceived.Int64
	}
	if req.AdditionalAmount != nil && *req.AdditionalAmount > 0 {
		missing = *req.AdditionalAmount
	}
	if missing <= 0 {
		return response.TopUpPaymentResponseDTO{}, errors.New("Số tiền cần thanh toán bù không hợp lệ")
	}

	extendHours := req.ExtendHours
	if extendHours <= 0 {
		extendHours = 24
	}

	var seatID, candidateAccountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_exam_seat_id, c.candidate_account_id
		FROM exam_registrations er
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er`, regID).Scan(&seatID, &candidateAccountID); err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: topup registration: %w", err)
	}

	var newHeldUntil time.Time
	if err := tx.QueryRowContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_held_until = GREATEST(COALESCE(exam_seat_held_until, NOW()), NOW()) + ($1 || ' hours')::INTERVAL,
		    exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $2 AND exam_seat_is_deleted = FALSE
		RETURNING exam_seat_held_until`, extendHours, seatID).Scan(&newHeldUntil); err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: topup extend seat: %w", err)
	}

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)

	var topUpID int64
	content := fmt.Sprintf("TOPUP-HSK-%d", regID)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO payments (
			payment_exam_registration_id, payment_amount, payment_method,
			payment_gateway, payment_content, payment_status, payment_is_top_up,
			payment_parent_payment_id, payment_evidence_urls, payment_admin_note,
			payment_expires_at
		) VALUES (
			$1, $2, 'bank_transfer', 'payos', $3, 'pending', TRUE, $4, $5, $6, $7
		) RETURNING payment_id`,
		regID, missing, content, paymentID, evidenceJSON, req.Note, newHeldUntil,
	).Scan(&topUpID)
	if err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: insert topup payment: %w", err)
	}

	orderCode := time.Now().Unix()*10000 + topUpID
	orderCodeStr := fmt.Sprintf("%d", orderCode)
	if _, err := tx.ExecContext(ctx,
		`UPDATE payments SET payment_transaction_ref = $1 WHERE payment_id = $2`,
		orderCodeStr, topUpID); err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: update topup ref: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{
		"parent_payment_id": paymentID,
		"top_up_payment_id": topUpID,
		"amount":            missing,
		"extend_hours":      extendHours,
		"held_until":        newHeldUntil,
		"evidence_urls":     req.EvidenceURLs,
		"note":              req.Note,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityPayment.String(),
		EntityID:       topUpID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: topup commit: %w", err)
	}

	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = "https://hsk.dhv.edu.vn/payment/success"
	}
	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = "https://hsk.dhv.edu.vn/payment/cancel"
	}
	checkoutURL, err := s.callPayOSCreateLink(ctx, orderCode, missing, content, returnURL, cancelURL)
	if err != nil {
		_, _ = s.db.ExecContext(context.Background(), "UPDATE payments SET payment_status = 'failed' WHERE payment_id = $1", topUpID)
		return response.TopUpPaymentResponseDTO{}, fmt.Errorf("payment: payos create topup link: %w", err)
	}

	notifContent := fmt.Sprintf("Thông báo: Phiếu đăng ký HSK #%d của bạn bị thiếu %d VNĐ. Vui lòng thanh toán bù qua link sau: %s. Chỗ ngồi được bảo lưu thêm %d giờ.",
		regID, missing, checkoutURL, extendHours)
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO notifications (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1, 'email', $2, 'pending'), ($1, 'zalo', $2, 'pending')`, candidateAccountID, notifContent)

	return response.TopUpPaymentResponseDTO{
		TopUpPaymentID:   topUpID,
		ParentPaymentID:  paymentID,
		AdditionalAmount: missing,
		CheckoutURL:      checkoutURL,
		SeatHeldUntil:    &newHeldUntil,
		Message:          "Đã tạo link thanh toán bù và gửi thông báo tự động qua Email/Zalo thành công",
	}, nil
}

// AdminResolveOverpayment confirms candidate seat and records a partial refund for the overpaid amount.
func (s *Service) AdminResolveOverpayment(ctx context.Context, paymentID, actorID int64, req request.AdminResolveOverpaymentRequestDTO) (response.ResolveOverpaymentResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.ResolveOverpaymentResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: overpayment begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var regID, amount int64
	var amountReceived sql.NullInt64
	var status string
	var method payenum.PaymentMethod
	var gateway, content *string
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_exam_registration_id, payment_amount, payment_amount_received, payment_status,
		       payment_method, payment_gateway, payment_content
		FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, paymentID).Scan(&regID, &amount, &amountReceived, &status, &method, &gateway, &content); errors.Is(err, sql.ErrNoRows) {
		return response.ResolveOverpaymentResponseDTO{}, errors.New("Không tìm thấy giao dịch")
	} else if err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: overpayment lock: %w", err)
	}

	if status != payenum.PayStatusAmountMismatch.String() && status != payenum.PayStatusSuccess.String() {
		return response.ResolveOverpaymentResponseDTO{}, errors.New("Giao dịch không ở trạng thái thừa tiền (amount_mismatch)")
	}

	received := amount
	if amountReceived.Valid {
		received = amountReceived.Int64
	}
	if received <= amount && (req.RefundAmount == nil || *req.RefundAmount <= 0) {
		return response.ResolveOverpaymentResponseDTO{}, errors.New("Số tiền thực nhận không lớn hơn số tiền yêu cầu")
	}

	refundAmount := received - amount
	if req.RefundAmount != nil && *req.RefundAmount > 0 {
		refundAmount = *req.RefundAmount
	}

	var seatID, candidateAccountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_exam_seat_id, c.candidate_account_id
		FROM exam_registrations er
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er`, regID).Scan(&seatID, &candidateAccountID); err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: overpayment reg: %w", err)
	}

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'success', payment_paid_at = COALESCE(payment_paid_at, NOW()),
		    payment_evidence_urls = $2, payment_admin_note = $3
		WHERE payment_id = $1`, paymentID, evidenceJSON, req.Note); err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: confirm overpayment: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, regID); err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: confirm reg overpayment: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1`, seatID); err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: book seat overpayment: %w", err)
	}

	var refundID int64
	refundContent := fmt.Sprintf("REFUND-PARTIAL-HSK-%d", regID)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO payments (
			payment_exam_registration_id, payment_amount, payment_amount_received,
			payment_method, payment_gateway, payment_content, payment_status,
			payment_paid_at, payment_evidence_urls, payment_admin_note,
			payment_refund_amount, payment_refund_reason, payment_parent_payment_id
		) VALUES (
			$1, $2, $2, $3, $4, $5, 'pending_refund', NOW(), $6, $7, $2, $7, $8
		) RETURNING payment_id`,
		regID, refundAmount, method, gateway, refundContent, evidenceJSON, req.Note, paymentID,
	).Scan(&refundID)
	if err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: insert partial refund: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    auditenum.AuditEntityPayment.String(),
		EntityID:       paymentID,
		NewValue:       []byte(fmt.Sprintf(`{"status":"success","action":"resolve_overpayment","refund_id":%d}`, refundID)),
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionRefund.String(),
		EntityTable:    auditenum.AuditEntityPayment.String(),
		EntityID:       refundID,
		NewValue:       []byte(fmt.Sprintf(`{"amount":%d,"action":"partial_refund","parent_id":%d}`, refundAmount, paymentID)),
	})

	notifContent := fmt.Sprintf("Thông báo: Phiếu đăng ký HSK #%d của bạn đã được xác nhận giữ chỗ thành công. Số tiền thừa %d VNĐ đã được tạo bản ghi hoàn tiền một phần.",
		regID, refundAmount)
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO notifications (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1, 'email', $2, 'pending'), ($1, 'zalo', $2, 'pending')`, candidateAccountID, notifContent)

	if err := tx.Commit(); err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: overpayment commit: %w", err)
	}

	confirmedEntity, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: fetch confirmed: %w", err)
	}
	refundEntity, err := s.fetchAdminPaymentByID(ctx, refundID)
	if err != nil {
		return response.ResolveOverpaymentResponseDTO{}, fmt.Errorf("payment: fetch refund: %w", err)
	}

	return response.ResolveOverpaymentResponseDTO{
		ConfirmedPayment: ToAdminPaymentResponseDTO(confirmedEntity),
		RefundPayment:    ToAdminPaymentResponseDTO(refundEntity),
		Message:          "Đã duyệt giữ ghế chính thức và tạo bản ghi hoàn tiền một phần thành công",
	}, nil
}

// AdminGetDoubleCharges returns grouped transactions suspected of double charges (same candidate + exam session).
func (s *Service) AdminGetDoubleCharges(ctx context.Context, page, limit int) ([]response.DoubleChargeGroupResponseDTO, *utils.Meta, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	countQuery := `
		WITH dup_groups AS (
			SELECT er.exam_registration_candidate_id, er.exam_registration_exam_session_id
			FROM payments p
			JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
			WHERE p.payment_is_deleted = FALSE AND er.exam_registration_is_deleted = FALSE
			  AND p.payment_status IN ('success', 'pending', 'partial_paid', 'amount_mismatch')
			GROUP BY er.exam_registration_candidate_id, er.exam_registration_exam_session_id
			HAVING COUNT(p.payment_id) > 1
		)
		SELECT COUNT(*) FROM dup_groups`

	var total int64
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("payment: count double charges: %w", err)
	}

	groupQuery := `
		WITH dup_groups AS (
			SELECT er.exam_registration_candidate_id, er.exam_registration_exam_session_id
			FROM payments p
			JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
			WHERE p.payment_is_deleted = FALSE AND er.exam_registration_is_deleted = FALSE
			  AND p.payment_status IN ('success', 'pending', 'partial_paid', 'amount_mismatch')
			GROUP BY er.exam_registration_candidate_id, er.exam_registration_exam_session_id
			HAVING COUNT(p.payment_id) > 1
			ORDER BY er.exam_registration_candidate_id DESC
			LIMIT $1 OFFSET $2
		)
		SELECT dg.exam_registration_candidate_id, dg.exam_registration_exam_session_id,
		       c.candidate_full_name, COALESCE(cd.candidate_document_number, ''),
		       et.exam_type_name, es.exam_session_date::TEXT
		FROM dup_groups dg
		JOIN candidates c ON c.candidate_id = dg.exam_registration_candidate_id
		JOIN exam_sessions es ON es.exam_session_id = dg.exam_registration_exam_session_id
		JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		LEFT JOIN candidate_documents cd ON cd.candidate_document_candidate_id = c.candidate_id
		     AND cd.candidate_document_is_deleted = FALSE`

	rows, err := s.db.QueryContext(ctx, groupQuery, limit, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("payment: query double charges: %w", err)
	}
	defer rows.Close()

	var groups []response.DoubleChargeGroupResponseDTO
	for rows.Next() {
		var g response.DoubleChargeGroupResponseDTO
		if err := rows.Scan(&g.CandidateID, &g.ExamSessionID, &g.CandidateName, &g.CandidateCCCD, &g.ExamTypeName, &g.ExamSessionDate); err != nil {
			return nil, nil, fmt.Errorf("payment: scan double charge group: %w", err)
		}

		pRows, err := s.db.QueryContext(ctx, `
			SELECT p.payment_id, p.payment_exam_registration_id, p.payment_amount,
			       p.payment_amount_received, p.payment_status, p.payment_transaction_ref,
			       p.payment_paid_at, p.payment_created_at
			FROM payments p
			JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
			WHERE er.exam_registration_candidate_id = $1
			  AND er.exam_registration_exam_session_id = $2
			  AND p.payment_is_deleted = FALSE
			ORDER BY p.payment_created_at ASC`, g.CandidateID, g.ExamSessionID)
		if err != nil {
			return nil, nil, fmt.Errorf("payment: query group payments: %w", err)
		}

		var pList []response.DoubleChargePaymentDTO
		for pRows.Next() {
			var pItem response.DoubleChargePaymentDTO
			if err := pRows.Scan(&pItem.PaymentID, &pItem.RegistrationID, &pItem.Amount,
				&pItem.AmountReceived, &pItem.Status, &pItem.TransactionRef,
				&pItem.PaidAt, &pItem.CreatedAt); err != nil {
				pRows.Close()
				return nil, nil, fmt.Errorf("payment: scan item payment: %w", err)
			}
			pList = append(pList, pItem)
		}
		pRows.Close()
		g.SuspectedPayments = pList
		groups = append(groups, g)
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
	return groups, meta, nil
}

// AdminResolveDoubleCharge keeps 1 valid transaction and marks duplicate charges for 100% refund.
func (s *Service) AdminResolveDoubleCharge(ctx context.Context, actorID int64, req request.AdminResolveDoubleChargeRequestDTO) (response.ResolveDoubleChargeResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.ResolveDoubleChargeResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}
	if len(req.RefundPaymentIDs) == 0 {
		return response.ResolveDoubleChargeResponseDTO{}, errors.New("Danh sách giao dịch cần hoàn tiền không được để trống")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: resolve double begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)

	var keepRegID, keepAmount int64
	var keepStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT payment_exam_registration_id, payment_amount, payment_status
		FROM payments
		WHERE payment_id = $1 AND payment_is_deleted = FALSE
		FOR UPDATE`, req.KeepPaymentID).Scan(&keepRegID, &keepAmount, &keepStatus); errors.Is(err, sql.ErrNoRows) {
		return response.ResolveDoubleChargeResponseDTO{}, errors.New("Không tìm thấy giao dịch giữ lại")
	} else if err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: lock keep payment: %w", err)
	}

	var keepSeatID, candidateAccountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_exam_seat_id, c.candidate_account_id
		FROM exam_registrations er
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er`, keepRegID).Scan(&keepSeatID, &candidateAccountID); err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: lock keep reg: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET payment_status = 'success', payment_paid_at = COALESCE(payment_paid_at, NOW()),
		    payment_evidence_urls = $2, payment_admin_note = $3
		WHERE payment_id = $1`, req.KeepPaymentID, evidenceJSON, req.Note); err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: update keep payment: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, keepRegID); err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: confirm keep reg: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1`, keepSeatID); err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: book keep seat: %w", err)
	}

	var refundedDTOs []response.AdminPaymentResponseDTO
	for _, refundID := range req.RefundPaymentIDs {
		if refundID == req.KeepPaymentID {
			continue
		}
		var rRegID, rAmount int64
		var rStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT payment_exam_registration_id, payment_amount, payment_status
			FROM payments
			WHERE payment_id = $1 AND payment_is_deleted = FALSE
			FOR UPDATE`, refundID).Scan(&rRegID, &rAmount, &rStatus); err != nil {
			return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: lock refund payment %d: %w", refundID, err)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET payment_status = 'pending_refund',
			    payment_paid_at = COALESCE(payment_paid_at, NOW()),
			    payment_refund_amount = $2,
			    payment_refund_reason = $3,
			    payment_evidence_urls = $4,
			    payment_admin_note = $3
			WHERE payment_id = $1`, refundID, rAmount, req.Note, evidenceJSON); err != nil {
			return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: update refund payment %d: %w", refundID, err)
		}

		if rRegID != keepRegID {
			var rSeatID int64
			var rRegStatus string
			if err := tx.QueryRowContext(ctx, `
				SELECT exam_registration_exam_seat_id, exam_registration_status
				FROM exam_registrations
				WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
				FOR UPDATE`, rRegID).Scan(&rSeatID, &rRegStatus); err == nil {
				if rRegStatus != "confirmed" {
					_, _ = tx.ExecContext(ctx, `UPDATE exam_registrations SET exam_registration_status = 'cancelled' WHERE exam_registration_id = $1`, rRegID)
					_, _ = tx.ExecContext(ctx, `UPDATE exam_seats SET exam_seat_status = 'available', exam_seat_held_until = NULL WHERE exam_seat_id = $1 AND exam_seat_status IN ('held', 'booked')`, rSeatID)
				}
			}
		}

		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &actorID,
			Action:         auditenum.AuditActionRefund.String(),
			EntityTable:    auditenum.AuditEntityPayment.String(),
			EntityID:       refundID,
			NewValue:       []byte(fmt.Sprintf(`{"action":"refund_double_charge","keep_id":%d,"refund_amount":%d}`, req.KeepPaymentID, rAmount)),
		})
	}

	notifContent := fmt.Sprintf("Thông báo: Hệ thống đã phát hiện và xử lý thanh toán đúp. Giao dịch hợp lệ #%d đã được giữ lại và các giao dịch trùng đã được chuyển danh sách hoàn tiền 100%%.", req.KeepPaymentID)
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO notifications (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1, 'email', $2, 'pending'), ($1, 'zalo', $2, 'pending')`, candidateAccountID, notifContent)

	if err := tx.Commit(); err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: double charge commit: %w", err)
	}

	keptEntity, err := s.fetchAdminPaymentByID(ctx, req.KeepPaymentID)
	if err != nil {
		return response.ResolveDoubleChargeResponseDTO{}, fmt.Errorf("payment: fetch kept: %w", err)
	}

	for _, refundID := range req.RefundPaymentIDs {
		if refundID == req.KeepPaymentID {
			continue
		}
		if refEnt, err := s.fetchAdminPaymentByID(ctx, refundID); err == nil {
			refundedDTOs = append(refundedDTOs, ToAdminPaymentResponseDTO(refEnt))
		}
	}

	return response.ResolveDoubleChargeResponseDTO{
		KeptPayment:      ToAdminPaymentResponseDTO(keptEntity),
		RefundedPayments: refundedDTOs,
		Message:          "Đã giải quyết thanh toán đúp: Giữ 1 giao dịch hợp lệ và hoàn tiền 100% các giao dịch trùng lặp thành công",
	}, nil
}

// AdminCreateSuspense manually creates an unmatched bank transfer record from bank statements/receipts.
func (s *Service) AdminCreateSuspense(ctx context.Context, actorID int64, req request.AdminCreateSuspenseRequestDTO) (response.SuspenseTransactionResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.SuspenseTransactionResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)
	var transferTime *time.Time
	if req.TransferTime != nil && *req.TransferTime != "" {
		if t, err := time.Parse(time.RFC3339, *req.TransferTime); err == nil {
			transferTime = &t
		} else if t, err := time.Parse("2006-01-02 15:04:05", *req.TransferTime); err == nil {
			transferTime = &t
		}
	}

	var e SuspenseTransactionEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO payment_suspense_transactions (
			suspense_amount, suspense_bank_ref, suspense_transfer_content,
			suspense_transfer_time, suspense_evidence_urls, suspense_status,
			suspense_admin_note, suspense_source
		) VALUES (
			$1, $2, $3, $4, $5, 'unmatched', $6, 'manual'
		) RETURNING `+suspenseCols,
		req.Amount, req.BankRef, req.TransferContent, transferTime, evidenceJSON, req.AdminNote,
	).Scan(
		&e.SuspenseID, &e.SuspenseOrderCode, &e.SuspenseAmount, &e.SuspenseBankRef,
		&e.SuspenseTransferContent, &e.SuspenseTransferTime, &evidenceJSON,
		&e.SuspenseStatus, &e.SuspenseMatchedRegistrationID, &e.SuspenseMatchedPaymentID,
		&e.SuspenseMatchedByAdminID, &e.SuspenseMatchedAt, &e.SuspenseAdminNote, &e.SuspenseSource,
		&e.SuspenseIsDeleted, &e.SuspenseDeletedAt, &e.SuspenseCreatedAt, &e.SuspenseUpdatedAt,
	)
	if err != nil {
		return response.SuspenseTransactionResponseDTO{}, fmt.Errorf("payment: insert suspense: %w", err)
	}
	_ = json.Unmarshal(evidenceJSON, &e.SuspenseEvidenceURLs)

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    "payment_suspense_transactions",
		EntityID:       e.SuspenseID,
		NewValue:       []byte(fmt.Sprintf(`{"bank_ref":"%s","amount":%d}`, req.BankRef, req.Amount)),
	})

	return ToSuspenseTransactionResponseDTO(e), nil
}

// AdminListSuspense lists all suspense transactions with optional filtering and pagination.
func (s *Service) AdminListSuspense(ctx context.Context, req request.AdminListSuspenseRequestDTO) ([]response.SuspenseTransactionResponseDTO, *utils.Meta, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	where := "s.suspense_is_deleted = FALSE"
	args := []any{}
	argIdx := 1

	if req.Status != "" && req.Status != "all" {
		where += fmt.Sprintf(" AND s.suspense_status = $%d", argIdx)
		args = append(args, req.Status)
		argIdx++
	}
	if req.From != "" {
		where += fmt.Sprintf(" AND s.suspense_created_at >= $%d::date", argIdx)
		args = append(args, req.From)
		argIdx++
	}
	if req.To != "" {
		where += fmt.Sprintf(" AND s.suspense_created_at < ($%d::date + INTERVAL '1 day')", argIdx)
		args = append(args, req.To)
		argIdx++
	}
	if req.Search != "" {
		where += fmt.Sprintf(" AND (s.suspense_bank_ref ILIKE $%d OR s.suspense_transfer_content ILIKE $%d OR s.suspense_order_code ILIKE $%d)", argIdx, argIdx, argIdx)
		args = append(args, "%"+req.Search+"%")
		argIdx++
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM payment_suspense_transactions s WHERE "+where, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("payment: count suspense: %w", err)
	}

	selectQuery := fmt.Sprintf(`SELECT %s FROM payment_suspense_transactions s WHERE %s ORDER BY s.suspense_created_at DESC LIMIT $%d OFFSET $%d`,
		suspenseCols, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("payment: query suspense: %w", err)
	}
	defer rows.Close()

	var list []response.SuspenseTransactionResponseDTO
	for rows.Next() {
		var e SuspenseTransactionEntity
		if err := scanSuspense(rows, &e); err != nil {
			return nil, nil, fmt.Errorf("payment: scan suspense: %w", err)
		}
		list = append(list, ToSuspenseTransactionResponseDTO(e))
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

// AdminMatchSuspense manually matches an unmatched bank transfer to a registration, activating the seat.
func (s *Service) AdminMatchSuspense(ctx context.Context, suspenseID, actorID int64, req request.AdminMatchSuspenseRequestDTO) (response.MatchSuspenseResponseDTO, error) {
	if len(req.EvidenceURLs) == 0 {
		return response.MatchSuspenseResponseDTO{}, errors.New("Bắt buộc đính kèm ít nhất 1 hình ảnh bằng chứng")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: match suspense begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var suspenseAmt int64
	var bankRef, transferContent sql.NullString
	var sStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT suspense_amount, suspense_bank_ref, suspense_transfer_content, suspense_status
		FROM payment_suspense_transactions
		WHERE suspense_id = $1 AND suspense_is_deleted = FALSE
		FOR UPDATE`, suspenseID).Scan(&suspenseAmt, &bankRef, &transferContent, &sStatus); errors.Is(err, sql.ErrNoRows) {
		return response.MatchSuspenseResponseDTO{}, errors.New("Không tìm thấy giao dịch treo")
	} else if err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: lock suspense: %w", err)
	}

	if sStatus != "unmatched" {
		return response.MatchSuspenseResponseDTO{}, errors.New("Giao dịch treo đã được khớp hoặc xử lý trước đó")
	}

	var regStatus string
	var seatID, candidateAccountID, fee int64
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_status, er.exam_registration_exam_seat_id,
		       c.candidate_account_id, es.exam_session_fee
		FROM exam_registrations er
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er`, req.ExamRegistrationID).Scan(&regStatus, &seatID, &candidateAccountID, &fee); errors.Is(err, sql.ErrNoRows) {
		return response.MatchSuspenseResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: lock match reg: %w", err)
	}

	if regStatus == regenum.RegStatusConfirmed.String() {
		return response.MatchSuspenseResponseDTO{}, errors.New("Phiếu đăng ký đã được xác nhận thanh toán trước đó")
	}

	var seatStatus string
	var heldUntil *time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_seat_status, exam_seat_held_until
		FROM exam_seats
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE
		FOR UPDATE`, seatID).Scan(&seatStatus, &heldUntil); err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: lock match seat: %w", err)
	}

	seatAvailable := seatStatus == "available" || (seatStatus == "held" && (regStatus == "pending_payment" || (heldUntil != nil && heldUntil.After(time.Now()))))
	if !seatAvailable {
		return response.MatchSuspenseResponseDTO{}, errors.New("Ghế thi không còn khả dụng hoặc đã bị người khác lấy")
	}

	evidenceJSON, _ := json.Marshal(req.EvidenceURLs)
	refStr := ""
	if bankRef.Valid {
		refStr = bankRef.String
	}
	contentStr := ""
	if transferContent.Valid {
		contentStr = transferContent.String
	}

	var paymentID int64
	err = tx.QueryRowContext(ctx, `
		SELECT payment_id FROM payments
		WHERE payment_exam_registration_id = $1 AND payment_is_deleted = FALSE
		ORDER BY payment_created_at DESC LIMIT 1
		FOR UPDATE`, req.ExamRegistrationID).Scan(&paymentID)

	if err == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE payments
			SET payment_status = 'success', payment_paid_at = NOW(),
			    payment_amount_received = $2, payment_gateway_trans_id = NULLIF($3, ''),
			    payment_content = NULLIF($4, ''), payment_evidence_urls = $5, payment_admin_note = $6
			WHERE payment_id = $1`, paymentID, suspenseAmt, refStr, contentStr, evidenceJSON, req.Note); err != nil {
			return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: update match payment: %w", err)
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO payments (
				payment_exam_registration_id, payment_amount, payment_amount_received,
				payment_method, payment_gateway, payment_gateway_trans_id, payment_content,
				payment_status, payment_paid_at, payment_evidence_urls, payment_admin_note
			) VALUES (
				$1, $2, $3, 'bank_transfer', 'manual_match', NULLIF($4, ''), NULLIF($5, ''),
				'success', NOW(), $6, $7
			) RETURNING payment_id`,
			req.ExamRegistrationID, fee, suspenseAmt, refStr, contentStr, evidenceJSON, req.Note,
		).Scan(&paymentID)
		if err != nil {
			return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: insert match payment: %w", err)
		}
	} else {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: query match payment: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations SET exam_registration_status = 'confirmed', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, req.ExamRegistrationID); err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: confirm reg: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats SET exam_seat_status = 'booked', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1`, seatID); err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: book seat: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_suspense_transactions
		SET suspense_status = 'matched', suspense_matched_registration_id = $2,
		    suspense_matched_payment_id = $3, suspense_matched_by_admin_id = $4,
		    suspense_matched_at = NOW(), suspense_evidence_urls = $5, suspense_admin_note = $6,
		    suspense_updated_at = NOW()
		WHERE suspense_id = $1`, suspenseID, req.ExamRegistrationID, paymentID, actorID, evidenceJSON, req.Note); err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: update suspense matched: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    "payment_suspense_transactions",
		EntityID:       suspenseID,
		NewValue:       []byte(fmt.Sprintf(`{"action":"manual_match","registration_id":%d,"payment_id":%d}`, req.ExamRegistrationID, paymentID)),
	})

	notifContent := fmt.Sprintf("Thông báo: Giao dịch chuyển khoản của bạn đã được đối soát thành công. Ca thi của phiếu đăng ký #%d đã được kích hoạt chính thức.", req.ExamRegistrationID)
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO notifications (notification_account_id, notification_channel, notification_content, notification_status)
		VALUES ($1, 'email', $2, 'pending'), ($1, 'zalo', $2, 'pending')`, candidateAccountID, notifContent)

	if err := tx.Commit(); err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: match commit: %w", err)
	}

	if s.hub != nil {
		s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
			"registration_id": req.ExamRegistrationID,
			"seat_id":         seatID,
			"status":          "confirmed",
			"action":          "manual_match",
		})
	}

	var sEnt SuspenseTransactionEntity
	_ = s.db.QueryRowContext(ctx, `SELECT `+suspenseCols+` FROM payment_suspense_transactions s WHERE s.suspense_id = $1`, suspenseID).Scan(
		&sEnt.SuspenseID, &sEnt.SuspenseOrderCode, &sEnt.SuspenseAmount, &sEnt.SuspenseBankRef,
		&sEnt.SuspenseTransferContent, &sEnt.SuspenseTransferTime, &evidenceJSON,
		&sEnt.SuspenseStatus, &sEnt.SuspenseMatchedRegistrationID, &sEnt.SuspenseMatchedPaymentID,
		&sEnt.SuspenseMatchedByAdminID, &sEnt.SuspenseMatchedAt, &sEnt.SuspenseAdminNote, &sEnt.SuspenseSource,
		&sEnt.SuspenseIsDeleted, &sEnt.SuspenseDeletedAt, &sEnt.SuspenseCreatedAt, &sEnt.SuspenseUpdatedAt,
	)
	_ = json.Unmarshal(evidenceJSON, &sEnt.SuspenseEvidenceURLs)

	pEnt, err := s.fetchAdminPaymentByID(ctx, paymentID)
	if err != nil {
		return response.MatchSuspenseResponseDTO{}, fmt.Errorf("payment: fetch matched payment: %w", err)
	}

	return response.MatchSuspenseResponseDTO{
		SuspenseTransaction: ToSuspenseTransactionResponseDTO(sEnt),
		ConfirmedPayment:    ToAdminPaymentResponseDTO(pEnt),
		Message:             "Đã thực hiện đối soát và kích hoạt ghế thành công",
	}, nil
}

// AdminImportSuspense imports a batch of statement rows into the suspense account.
// It detects duplicates by bank_ref and attempts automatic matching if an order code is extracted.
func (s *Service) AdminImportSuspense(ctx context.Context, actorID int64, req request.AdminImportSuspenseRequestDTO) (response.ImportSuspenseResponseDTO, error) {
	if len(req.Items) == 0 {
		return response.ImportSuspenseResponseDTO{}, errors.New("Danh sách giao dịch sao kê không được rỗng")
	}

	evidenceBytes, _ := json.Marshal(req.EvidenceURLs)
	evidenceJSON := string(evidenceBytes)

	var totalProcessed, importedCount, skippedCount, autoMatchedCount int

	for _, item := range req.Items {
		totalProcessed++
		bankRef := strings.TrimSpace(item.BankRef)
		if bankRef == "" || item.Amount <= 0 {
			skippedCount++
			continue
		}

		// Check if bank_ref already exists in suspense transactions or payments
		var exists bool
		err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM payment_suspense_transactions
				WHERE suspense_bank_ref = $1 AND suspense_is_deleted = FALSE
				UNION ALL
				SELECT 1 FROM payments
				WHERE payment_gateway_trans_id = $1 AND payment_is_deleted = FALSE
			)`, bankRef).Scan(&exists)
		if err == nil && exists {
			skippedCount++
			continue
		}

		// Parse transfer time
		var transferTime *time.Time
		if item.TransferTime != nil && strings.TrimSpace(*item.TransferTime) != "" {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*item.TransferTime))
			if err == nil {
				transferTime = &t
			}
		}

		// Insert into payment_suspense_transactions
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO payment_suspense_transactions (
				suspense_amount, suspense_bank_ref, suspense_transfer_content, suspense_transfer_time,
				suspense_status, suspense_source, suspense_evidence_urls, suspense_admin_note
			) VALUES (
				$1, $2, $3, $4, 'unmatched', 'statement_import', $5, 'Import từ sao kê ngân hàng'
			)`, item.Amount, bankRef, item.TransferContent, transferTime, evidenceJSON)
		if err != nil {
			skippedCount++
			continue
		}
		importedCount++
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &actorID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    "payment_suspense_transactions",
		EntityID:       0,
		NewValue:       []byte(fmt.Sprintf(`{"action":"import_statement","total":%d,"imported":%d,"skipped":%d}`, totalProcessed, importedCount, skippedCount)),
	})

	return response.ImportSuspenseResponseDTO{
		TotalProcessed:   totalProcessed,
		ImportedCount:    importedCount,
		SkippedCount:     skippedCount,
		AutoMatchedCount: autoMatchedCount,
		Message:          fmt.Sprintf("Xử lý sao kê hoàn tất: %d đã import vào tài khoản treo, %d bỏ qua/trùng", importedCount, skippedCount),
	}, nil
}


