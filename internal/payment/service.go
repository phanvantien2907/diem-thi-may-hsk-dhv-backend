package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
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

// ─── Column list + scanner (DRY) ───────────────────────────────────────────

const paymentCols = `payment_id, payment_exam_registration_id, payment_amount, payment_method,
	payment_gateway, payment_transaction_ref, payment_gateway_trans_id, payment_content,
	payment_status, payment_expires_at, payment_paid_at,
	payment_is_deleted, payment_deleted_at, payment_created_at, payment_updated_at`

func scanPayment(row interface{ Scan(dest ...any) error }, e *PaymentEntity) error {
	return row.Scan(
		&e.PaymentID, &e.PaymentExamRegistrationID, &e.PaymentAmount, &e.PaymentMethod,
		&e.PaymentGateway, &e.PaymentTransactionRef, &e.PaymentGatewayTransID, &e.PaymentContent,
		&e.PaymentStatus, &e.PaymentExpiresAt, &e.PaymentPaidAt,
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
	orderCodeStr := fmt.Sprintf("%d", e.PaymentID)
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
		"order_code":      e.PaymentID,
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		Action:      auditenum.AuditActionPayment.String(),
		EntityTable: auditenum.AuditEntityPayment.String(),
		EntityID:    e.PaymentID,
		NewValue:    newVal,
	})

	// 6. GIẢI PHÓNG CONNECTION DB TRƯỚC KHI GỌI MẠNG (Tối ưu điểm nghẽn)
	if err := tx.Commit(); err != nil {
		return response.PaymentResponseDTO{}, fmt.Errorf("payment: commit: %w", err)
	}

	// 7. Gọi PayOS API để lấy Link (Mất 200-300ms, KHÔNG chiếm dụng DB Connection)
	checkoutURL, err := s.callPayOSCreateLink(ctx, e.PaymentID, amount, content)
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
		if !payenum.PaymentStatus(req.Status).Valid() {
			return nil, nil, errors.New("Trạng thái thanh toán không hợp lệ")
		}
		where += fmt.Sprintf(" AND p.payment_status = $%d", argIdx)
		args = append(args, req.Status)
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
		paymentCols, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("payment: admin list: %w", err)
	}
	defer rows.Close()

	var list []response.AdminPaymentResponseDTO
	for rows.Next() {
		var e PaymentEntity
		if err := scanPayment(rows, &e); err != nil {
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
func (s *Service) callPayOSCreateLink(ctx context.Context, orderCode int64, amount int64, description string) (string, error) {
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
		"cancelUrl": "https://hsk-vinh.edu.vn/payment/cancel",
		"returnUrl": "https://hsk-vinh.edu.vn/payment/success",
	}

	// Generate signature: sort keys alphabetically, join as key=value&, HMAC-SHA256.
	sigData := fmt.Sprintf("amount=%d&cancelUrl=%s&description=%s&orderCode=%d&returnUrl=%s",
		amount, "https://hsk-vinh.edu.vn/payment/cancel", description, orderCode, "https://hsk-vinh.edu.vn/payment/success")
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
