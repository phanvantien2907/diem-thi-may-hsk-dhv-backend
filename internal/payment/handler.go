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

// CreatePayment POST /payments
// @Summary Tạo yêu cầu thanh toán
// @Description Tạo giao dịch thanh toán (VNPay, Momo...) cho một đơn đăng ký.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.CreatePaymentRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /payments [post]
func (h *Handler) CreatePayment(c *gin.Context) {
	var req request.CreatePaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	p, err := h.svc.CreatePayment(c.Request.Context(), req)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.Created(c, p)
}

// Callback POST /payments/callback
// @Summary Xử lý Webhook Thanh toán
// @Description Endpoint nhận IPN/Callback từ cổng thanh toán để cập nhật trạng thái tự động.
// @Tags Payment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.PaymentCallbackRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 409 {object} map[string]interface{} "Giao dịch đã được xử lý"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /payments/callback [post]
func (h *Handler) Callback(c *gin.Context) {
	var req request.PaymentCallbackRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	p, err := h.svc.ProcessCallback(c.Request.Context(), req)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, p)
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
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
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
