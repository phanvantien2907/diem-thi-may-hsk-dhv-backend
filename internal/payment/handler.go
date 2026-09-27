package payment

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/payment/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the payment Service.
type Handler struct{ svc *Service }

// NewHandler creates a payment Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreatePayOSLink POST /payments/payos/create
// @Summary Tạo link thanh toán PayOS
// @Description Thí sinh tạo link thanh toán qua PayOS cho phiếu đăng ký. Trả về checkout URL.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.CreatePaymentRequestDTO true "Dữ liệu yêu cầu"
// @Success 201 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 409 {object} map[string]interface{} "Đã tồn tại giao dịch chờ thanh toán"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /payments/payos/create [post]
func (h *Handler) CreatePayOSLink(c *gin.Context) {
	var req request.CreatePaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	p, err := h.svc.CreatePayOSLink(c.Request.Context(), req)
	if err != nil {
		// Detect conflict errors (already has pending payment, already confirmed).
		errMsg := err.Error()
		if errMsg == "Đã tồn tại giao dịch chờ thanh toán cho phiếu đăng ký này" ||
			errMsg == "Phiếu đăng ký đã được xác nhận thanh toán" ||
			errMsg == "Không thể thanh toán cho phiếu đăng ký đã hủy" {
			utils.Conflict(c, errMsg)
			return
		}
		if errMsg == "Không tìm thấy phiếu đăng ký" {
			utils.NotFound(c, errMsg)
			return
		}
		
		importLog := func(err error) {
			println("====> PAYOS ERROR:", err.Error())
		}
		importLog(err)
		
		utils.InternalServerError(c, "Tạo link thanh toán thất bại")
		return
	}
	utils.Created(c, p)
}

// Callback POST /payments/callback
// @Summary Webhook PayOS
// @Description Endpoint nhận IPN từ PayOS. Idempotent — luôn trả 200.
// @Tags Payment
// @Accept json
// @Produce json
// @Param request body request.PayOSWebhookPayload true "Webhook payload from PayOS"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chữ ký không hợp lệ"
// @Router /payments/callback [post]
func (h *Handler) Callback(c *gin.Context) {
	var payload request.PayOSWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		// Even on parse error, return 200 to prevent PayOS from retrying garbage.
		utils.OK(c, gin.H{"message": "Webhook received"})
		return
	}

	if err := h.svc.ProcessWebhook(c.Request.Context(), payload); err != nil {
		errMsg := err.Error()
		if errMsg == "signature: invalid signature" {
			utils.Unauthorized(c, "Chữ ký không hợp lệ")
			return
		}
		if errMsg == "amount mismatch" {
			utils.BadRequest(c, "Số tiền thanh toán không khớp")
			return
		}
		// For any other internal error, still return 200 to avoid PayOS infinite retry.
		// The error is logged server-side by the service.
		utils.OK(c, gin.H{"message": "Webhook received"})
		return
	}

	utils.OK(c, gin.H{"message": "Webhook received"})
}

// GetPaymentHistory GET /payments?candidate_id=X
// @Summary Lịch sử thanh toán
// @Description Xem danh sách các giao dịch thanh toán của thí sinh.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /payments [get]
func (h *Handler) GetPaymentHistory(c *gin.Context) {
	var q struct {
		CandidateID int64 `form:"candidate_id"`
	}
	_ = c.ShouldBindQuery(&q)
	if q.CandidateID <= 0 {
		utils.BadRequest(c, "Tham số candidate_id là bắt buộc")
		return
	}
	list, err := h.svc.GetPaymentHistory(c.Request.Context(), q.CandidateID)
	if err != nil {
		utils.InternalServerError(c, "Lấy lịch sử giao dịch thất bại")
		return
	}
	utils.OK(c, list)
}

// ─── Admin Handlers ─────────────────────────────────────────────────────────

// AdminListPayments GET /admin/payments
// @Summary [Admin] Danh sách giao dịch
// @Description Lấy danh sách toàn bộ giao dịch (phân trang, lọc theo trạng thái).
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int false "Trang" default(1)
// @Param limit query int false "Số lượng mỗi trang" default(20)
// @Param status query string false "Lọc theo trạng thái (pending, success, failed, refunded)"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments [get]
func (h *Handler) AdminListPayments(c *gin.Context) {
	var req request.AdminListPaymentsRequestDTO
	_ = c.ShouldBindQuery(&req)

	list, meta, err := h.svc.AdminListPayments(c.Request.Context(), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Trạng thái thanh toán không hợp lệ" {
			utils.BadRequest(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Lấy danh sách giao dịch thất bại")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminConfirmPayment POST /admin/payments/:id/confirm
// @Summary [Admin] Xác nhận thủ công
// @Description Admin xác nhận thanh toán thủ công (chuyển pending → success).
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Giao dịch không ở trạng thái chờ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/{id}/confirm [post]
func (h *Handler) AdminConfirmPayment(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	result, err := h.svc.AdminConfirmPayment(c.Request.Context(), id)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, errMsg)
			return
		}
		// State machine violations.
		if len(errMsg) > 0 && errMsg[0] == 'G' {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Xác nhận giao dịch thất bại")
		return
	}
	utils.OK(c, result)
}

// AdminCancelPayment POST /admin/payments/:id/cancel
// @Summary [Admin] Hủy giao dịch
// @Description Admin hủy giao dịch (pending→failed, success→refunded).
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Trạng thái không cho phép hủy"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/{id}/cancel [post]
func (h *Handler) AdminCancelPayment(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	result, err := h.svc.AdminCancelPayment(c.Request.Context(), id)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, errMsg)
			return
		}
		if len(errMsg) > 20 {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Hủy giao dịch thất bại")
		return
	}
	utils.OK(c, result)
}
