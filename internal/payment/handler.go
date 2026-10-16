package payment

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

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
		if strings.Contains(errMsg, "không hợp lệ") || strings.Contains(errMsg, "YYYY-MM-DD") {
			utils.BadRequest(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Lấy danh sách giao dịch thất bại")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminExportPayments exports the filtered accounting list as UTF-8 CSV.
// @Summary [Admin] Xuất đối soát giao dịch
// @Tags Payment
// @Produce text/csv
// @Security BearerAuth
// @Router /admin/payments/export [get]
func (h *Handler) AdminExportPayments(c *gin.Context) {
	var req request.AdminListPaymentsRequestDTO
	_ = c.ShouldBindQuery(&req)
	data, err := h.svc.ExportPaymentsCSV(c.Request.Context(), req)
	if err != nil {
		if strings.Contains(err.Error(), "không hợp lệ") || strings.Contains(err.Error(), "YYYY-MM-DD") {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Xuất danh sách giao dịch thất bại")
		return
	}
	c.Data(200, "text/csv; charset=utf-8", data)
}

// AdminConfirmPayment POST /admin/payments/:id/confirm
// @Summary [Admin] Xác nhận thủ công
// @Description Admin xác nhận thanh toán thủ công (chuyển pending → success). Cần đính kèm bằng chứng.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminConfirmPaymentRequestDTO true "Dữ liệu xác nhận kèm bằng chứng"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
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
	var req request.AdminConfirmPaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	result, err := h.svc.AdminConfirmPaymentAs(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, errMsg)
			return
		}
		// State machine violations.
		if strings.HasPrefix(errMsg, "Giao dịch") || strings.HasPrefix(errMsg, "Không còn") || strings.HasPrefix(errMsg, "Phiếu") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Xác nhận giao dịch thất bại")
		return
	}
	utils.OK(c, result)
}

// AdminSyncPayment reconciles one payment against the PayOS payment API.
// @Summary [Admin] Đồng bộ trạng thái PayOS
// @Tags Payment
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Router /admin/payments/{id}/sync [post]
func (h *Handler) AdminSyncPayment(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	result, err := h.svc.SyncPayment(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		msg := err.Error()
		if msg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, msg)
		} else if strings.Contains(msg, "PayOS chưa") || strings.Contains(msg, "không khớp") || strings.Contains(msg, "Không còn") {
			utils.Conflict(c, msg)
		} else {
			utils.InternalServerError(c, "Đồng bộ trạng thái PayOS thất bại")
		}
		return
	}
	utils.OK(c, result)
}

// AdminMarkRefund marks a payment for manual bank refund processing.
// @Summary [Admin] Đánh dấu cần hoàn tiền
// @Description Đánh dấu giao dịch cần hoàn tiền với lý do và hình ảnh bằng chứng.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminMarkRefundRequestDTO true "Dữ liệu đánh dấu hoàn tiền"
// @Router /admin/payments/{id}/mark-refund [post]
func (h *Handler) AdminMarkRefund(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.AdminMarkRefundRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	result, err := h.svc.AdminMarkRefund(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		msg := err.Error()
		if msg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, msg)
		} else if strings.HasPrefix(msg, "Giao dịch") {
			utils.Conflict(c, msg)
		} else {
			utils.InternalServerError(c, "Đánh dấu hoàn tiền thất bại")
		}
		return
	}
	utils.OK(c, result)
}

// AdminCancelPayment POST /admin/payments/:id/cancel
// @Summary [Admin] Hủy giao dịch
// @Description Admin hủy giao dịch trong một transaction ACID kèm hình ảnh bằng chứng và lý do.
// @Description Với payment success, hệ thống chuyển sang refunded, hủy hồ sơ và trả ghế nếu kỳ thi chưa bắt đầu.
// @Description Sau ngày thi (bao gồm ngày thi hiện tại), toàn bộ thao tác bị khóa và rollback.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminCancelPaymentRequestDTO true "Dữ liệu hủy giao dịch kèm bằng chứng"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
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
	var req request.AdminCancelPaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	result, err := h.svc.AdminCancelPaymentAs(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, errMsg)
			return
		}
		if errMsg == "Không tìm thấy phiếu đăng ký" {
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

// GetEvidenceUploadSignature GET /admin/payments/evidence-upload-signature
// @Summary [Admin] Lấy chữ ký tải ảnh bằng chứng
// @Description Trả về Cloudinary upload signature để frontend tải ảnh bằng chứng trực tiếp lên Cloudinary folder hsk/payment-evidence.
// @Tags Payment
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/evidence-upload-signature [get]
func (h *Handler) GetEvidenceUploadSignature(c *gin.Context) {
	sig, err := h.svc.GetEvidenceUploadSignature()
	if err != nil {
		utils.InternalServerError(c, "Không thể tạo chữ ký tải ảnh")
		return
	}
	utils.OK(c, sig)
}

// AdminTopUpPayment POST /admin/payments/:id/top-up
// @Summary [Admin] Tạo link nộp tiền bổ sung (Underpayment)
// @Description Khi thí sinh thanh toán thiếu, Admin tạo link thanh toán bổ sung PayOS, gia hạn giữ chỗ và gửi thông báo qua Email & Zalo.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminTopUpPaymentRequestDTO true "Dữ liệu bổ sung tiền"
// @Success 201 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Xung đột nghiệp vụ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/{id}/top-up [post]
func (h *Handler) AdminTopUpPayment(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.AdminTopUpPaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminTopUpPayment(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch gốc" || errMsg == "Không tìm thấy phiếu đăng ký" {
			utils.NotFound(c, errMsg)
			return
		}
		if strings.Contains(errMsg, "chưa thanh toán thành công") || strings.Contains(errMsg, "đã đủ hoặc vượt") || strings.Contains(errMsg, "không ở trạng thái") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, errMsg)
		return
	}
	utils.Created(c, res)
}

// AdminResolveOverpayment POST /admin/payments/:id/resolve-overpayment
// @Summary [Admin] Xử lý thanh toán thừa (Overpayment)
// @Description Giữ chỗ hợp lệ cho thí sinh và tạo bản ghi hoàn tiền phần chênh lệch thừa.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminResolveOverpaymentRequestDTO true "Dữ liệu xử lý thanh toán thừa"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Xung đột nghiệp vụ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/{id}/resolve-overpayment [post]
func (h *Handler) AdminResolveOverpayment(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.AdminResolveOverpaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminResolveOverpayment(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" || errMsg == "Không tìm thấy phiếu đăng ký" {
			utils.NotFound(c, errMsg)
			return
		}
		if strings.Contains(errMsg, "không thừa") || strings.Contains(errMsg, "vượt quá") || strings.Contains(errMsg, "đã được xử lý") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, errMsg)
		return
	}
	utils.OK(c, res)
}

// AdminGetDoubleCharges GET /admin/payments/double-charges
// @Summary [Admin] Danh sách cảnh báo giao dịch trùng (Double Charge)
// @Description Cảnh báo các thí sinh / ca thi có từ 2 giao dịch thành công trở lên.
// @Tags Payment
// @Produce json
// @Security BearerAuth
// @Param page query int false "Trang" default(1)
// @Param limit query int false "Số lượng mỗi trang" default(20)
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/double-charges [get]
func (h *Handler) AdminGetDoubleCharges(c *gin.Context) {
	var q struct {
		Page  int `form:"page"`
		Limit int `form:"limit"`
	}
	_ = c.ShouldBindQuery(&q)
	list, meta, err := h.svc.AdminGetDoubleCharges(c.Request.Context(), q.Page, q.Limit)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách trùng giao dịch thất bại")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminResolveDoubleCharge POST /admin/payments/resolve-double-charge
// @Summary [Admin] Giải quyết giao dịch trùng (Double Charge)
// @Description Giữ lại 1 giao dịch hợp lệ và đánh dấu hoàn tiền 100% cho các giao dịch trùng lặp còn lại.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.AdminResolveDoubleChargeRequestDTO true "Dữ liệu xử lý giao dịch trùng"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Xung đột nghiệp vụ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/resolve-double-charge [post]
func (h *Handler) AdminResolveDoubleCharge(c *gin.Context) {
	var req request.AdminResolveDoubleChargeRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminResolveDoubleCharge(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Không tìm thấy") {
			utils.NotFound(c, errMsg)
			return
		}
		if strings.Contains(errMsg, "không thuộc cùng") || strings.Contains(errMsg, "chưa thanh toán thành công") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, errMsg)
		return
	}
	utils.OK(c, res)
}

// AdminCreateSuspense POST /admin/payments/suspense
// @Summary [Admin] Thêm giao dịch treo / chuyển khoản sai nội dung
// @Description Admin nhập thông tin chuyển khoản không xác định để theo dõi trong tài khoản treo.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.AdminCreateSuspenseRequestDTO true "Dữ liệu giao dịch treo"
// @Success 201 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/suspense [post]
func (h *Handler) AdminCreateSuspense(c *gin.Context) {
	var req request.AdminCreateSuspenseRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminCreateSuspense(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		if strings.Contains(err.Error(), "RFC3339") {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Tạo giao dịch treo thất bại")
		return
	}
	utils.Created(c, res)
}

// AdminListSuspense GET /admin/payments/suspense
// @Summary [Admin] Danh sách giao dịch treo (Suspense Account)
// @Description Lấy danh sách giao dịch chuyển khoản sai nội dung / chưa khớp.
// @Tags Payment
// @Produce json
// @Security BearerAuth
// @Param page query int false "Trang" default(1)
// @Param limit query int false "Số lượng mỗi trang" default(20)
// @Param status query string false "Trạng thái (pending, matched, refunded)"
// @Param from query string false "Từ ngày (YYYY-MM-DD)"
// @Param to query string false "Đến ngày (YYYY-MM-DD)"
// @Param search query string false "Tìm kiếm (nội dung chuyển khoản hoặc mã giao dịch ngân hàng)"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/suspense [get]
func (h *Handler) AdminListSuspense(c *gin.Context) {
	var req request.AdminListSuspenseRequestDTO
	_ = c.ShouldBindQuery(&req)
	list, meta, err := h.svc.AdminListSuspense(c.Request.Context(), req)
	if err != nil {
		if strings.Contains(err.Error(), "YYYY-MM-DD") {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Lấy danh sách giao dịch treo thất bại")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminMatchSuspense POST /admin/payments/suspense/:id/match
// @Summary [Admin] Khớp giao dịch treo vào phiếu đăng ký
// @Description Khớp tiền từ tài khoản treo vào phiếu đăng ký của thí sinh, tạo giao dịch thanh toán thành công và giữ chỗ thi.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Suspense ID"
// @Param request body request.AdminMatchSuspenseRequestDTO true "Dữ liệu khớp giao dịch"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy bản ghi"
// @Failure 409 {object} map[string]interface{} "Xung đột nghiệp vụ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/suspense/{id}/match [post]
func (h *Handler) AdminMatchSuspense(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.AdminMatchSuspenseRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminMatchSuspense(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Không tìm thấy") {
			utils.NotFound(c, errMsg)
			return
		}
		if strings.Contains(errMsg, "đã được khớp") || strings.Contains(errMsg, "không ở trạng thái chờ") || strings.Contains(errMsg, "hết chỗ") || strings.Contains(errMsg, "Không đủ tiền") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, errMsg)
		return
	}
	utils.OK(c, res)
}

// AdminConfirmRefund POST /admin/payments/:id/confirm-refund
// @Summary [Admin] Xác nhận đã hoàn tiền
// @Description Kế toán/Admin xác nhận đã chuyển khoản trả lại tiền cho thí sinh thành công (chuyển pending_refund → refunded).
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Payment ID"
// @Param request body request.AdminConfirmRefundRequestDTO true "Dữ liệu xác nhận hoàn tiền"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy giao dịch"
// @Failure 409 {object} map[string]interface{} "Xung đột nghiệp vụ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/{id}/confirm-refund [post]
func (h *Handler) AdminConfirmRefund(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.AdminConfirmRefundRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.AdminConfirmRefund(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Không tìm thấy giao dịch" {
			utils.NotFound(c, errMsg)
			return
		}
		if strings.Contains(errMsg, "không ở trạng thái chờ hoàn tiền") {
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, errMsg)
		return
	}
	utils.OK(c, res)
}

// AdminImportSuspense POST /admin/payments/suspense/import
// @Summary [Admin] Import sao kê ngân hàng vào tài khoản treo
// @Description Hỗ trợ upload file CSV sao kê ngân hàng hoặc gửi danh sách JSON để tự động lưu vào tài khoản treo và đối soát tự động.
// @Tags Payment
// @Accept multipart/form-data,json
// @Produce json
// @Security BearerAuth
// @Param file formData file false "File CSV sao kê ngân hàng"
// @Param request body request.AdminImportSuspenseRequestDTO false "Dữ liệu JSON nếu không dùng file"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/payments/suspense/import [post]
func (h *Handler) AdminImportSuspense(c *gin.Context) {
	var req request.AdminImportSuspenseRequestDTO

	file, err := c.FormFile("file")
	if err == nil && file != nil {
		f, err := file.Open()
		if err != nil {
			utils.BadRequest(c, "Không thể mở file upload")
			return
		}
		defer f.Close()

		reader := csv.NewReader(f)
		reader.FieldsPerRecord = -1
		records, err := reader.ReadAll()
		if err != nil {
			utils.BadRequest(c, "File CSV không đúng định dạng: "+err.Error())
			return
		}

		if len(records) < 2 {
			utils.BadRequest(c, "File CSV không có dữ liệu giao dịch")
			return
		}

		timeCol, amtCol, refCol, contentCol := 0, 1, 2, 3
		startRow := 1
		header := records[0]
		for i, col := range header {
			colLower := strings.ToLower(strings.TrimSpace(col))
			if strings.Contains(colLower, "tiền") || strings.Contains(colLower, "amount") {
				amtCol = i
			} else if strings.Contains(colLower, "mã") || strings.Contains(colLower, "ref") || strings.Contains(colLower, "tham chiếu") {
				refCol = i
			} else if strings.Contains(colLower, "nội dung") || strings.Contains(colLower, "content") || strings.Contains(colLower, "diễn giải") {
				contentCol = i
			} else if strings.Contains(colLower, "ngày") || strings.Contains(colLower, "time") || strings.Contains(colLower, "date") {
				timeCol = i
			}
		}

		for i := startRow; i < len(records); i++ {
			row := records[i]
			if len(row) <= 1 || (len(row) > 0 && strings.TrimSpace(row[0]) == "") {
				continue
			}

			var item request.AdminImportSuspenseItemDTO
			if refCol < len(row) {
				item.BankRef = strings.TrimSpace(row[refCol])
			}
			if item.BankRef == "" {
				item.BankRef = fmt.Sprintf("CSV_ROW_%d_%d", i, time.Now().Unix())
			}

			if contentCol < len(row) {
				item.TransferContent = strings.TrimSpace(row[contentCol])
			}
			if timeCol < len(row) {
				tStr := strings.TrimSpace(row[timeCol])
				item.TransferTime = &tStr
			}

			if amtCol < len(row) {
				cleanAmt := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(row[amtCol]), ",", ""), ".", "")
				cleanAmt = strings.ReplaceAll(cleanAmt, "VND", "")
				cleanAmt = strings.ReplaceAll(cleanAmt, "đ", "")
				cleanAmt = strings.TrimSpace(cleanAmt)
				amt, _ := strconv.ParseInt(cleanAmt, 10, 64)
				item.Amount = amt
			}

			if item.Amount > 0 {
				req.Items = append(req.Items, item)
			}
		}
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
			return
		}
	}

	if len(req.Items) == 0 {
		utils.BadRequest(c, "Không tìm thấy dữ liệu giao dịch hợp lệ để import")
		return
	}

	res, err := h.svc.AdminImportSuspense(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, res)
}


