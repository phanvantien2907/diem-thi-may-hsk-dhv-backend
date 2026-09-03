package registration

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the registration Service.
type Handler struct{ svc *Service }

// NewHandler creates a registration Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ─────────────────────────────────────────────────────────────────────────────
// Candidate: Registration
// ─────────────────────────────────────────────────────────────────────────────

// ReserveSeat POST /me/registrations
// @Summary Giữ chỗ / Đăng ký thi
// @Description Chọn ca thi, giữ ghế (15 phút) và tạo đơn đăng ký thi trạng thái chờ thanh toán.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.ReserveSeatRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 409 {object} map[string]interface{} "Ghế đã có người giữ/đặt"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/registrations [post]
func (h *Handler) ReserveSeat(c *gin.Context) {
	var req request.ReserveSeatRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	candidateID := utils.MustAccountID(c)
	reg, err := h.svc.ReserveSeat(c.Request.Context(), req, candidateID)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, reg)
}

// GetMyRegistrations GET /me/registrations
// @Summary Danh sách đăng ký thi
// @Description Xem lịch sử đăng ký thi của bản thân.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/registrations [get]
func (h *Handler) GetMyRegistrations(c *gin.Context) {
	candidateID := utils.MustAccountID(c)
	list, err := h.svc.GetMyRegistrations(c.Request.Context(), candidateID)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách phiếu đăng ký thất bại")
		return
	}
	utils.OK(c, list)
}

// PaymentConfirm POST /me/registrations/:id/payment-confirm
func (h *Handler) PaymentConfirm(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	var req request.PaymentConfirmRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	candidateID := utils.MustAccountID(c)
	if err := h.svc.ConfirmPayment(c.Request.Context(), id, candidateID, req); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Xác nhận thanh toán thành công"})
}

// TransferRegistration POST /me/registrations/:id/transfer
// @Summary Chuyển ca thi
// @Description Thí sinh xin chuyển sang ca thi khác (nếu đủ điều kiện).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Param request body request.TransferRegistrationRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/registrations/:id/transfer [post]
func (h *Handler) TransferRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	var req request.TransferRegistrationRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	candidateID := utils.MustAccountID(c)
	reg, err := h.svc.TransferRegistration(c.Request.Context(), id, candidateID, req)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, reg)
}

// CancelRegistration POST /me/registrations/:id/cancel
// @Summary Hủy đăng ký thi
// @Description Thí sinh hủy đơn đăng ký thi (hoàn trả ghế trống).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/registrations/:id/cancel [post]
func (h *Handler) CancelRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	candidateID := utils.MustAccountID(c)
	if err := h.svc.CancelRegistrationByCandidate(c.Request.Context(), id, candidateID); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đã hủy phiếu đăng ký"})
}

// GetAdmissionSlip GET /me/registrations/:id/admission-slip
// @Summary Lấy Thẻ dự thi
// @Description Lấy thông tin thẻ dự thi (SBD, phòng thi, giờ thi) sau khi đã đăng ký thành công.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/registrations/:id/admission-slip [get]
func (h *Handler) GetAdmissionSlip(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	candidateID := utils.MustAccountID(c)
	slip, err := h.svc.GetAdmissionSlip(c.Request.Context(), id, candidateID)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, slip)
}

// ─────────────────────────────────────────────────────────────────────────────
// Candidate: Waitlist
// ─────────────────────────────────────────────────────────────────────────────

// JoinWaitlist POST /me/exam-sessions/:id/waitlist
// @Summary Đăng ký danh sách chờ
// @Description Đăng ký nhận thông báo khi có ghế trống trong ca thi đã đầy.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/exam-sessions/:id/waitlist [post]
func (h *Handler) JoinWaitlist(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	candidateID := utils.MustAccountID(c)
	entry, err := h.svc.JoinWaitlist(c.Request.Context(), sessionID, candidateID)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, entry)
}

// ─────────────────────────────────────────────────────────────────────────────
// Proctor: Attendance
// ─────────────────────────────────────────────────────────────────────────────

// MarkAttendance PATCH /proctor/exam-sessions/:id/attendance/:registration_id
// @Summary Điểm danh
// @Description Giám thị đánh dấu có mặt/vắng mặt của thí sinh tại phòng thi.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Param registration_id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /proctor/exam-sessions/:id/attendance/:registration_id [patch]
func (h *Handler) MarkAttendance(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	regID, ok := utils.ParamID(c, "registration_id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	proctorID := utils.MustAccountID(c)
	if err := h.svc.MarkAttendance(c.Request.Context(), sessionID, regID, proctorID); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Điểm danh thành công"})
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Operations
// ─────────────────────────────────────────────────────────────────────────────

// GetSessionDashboard GET /admin/exam-sessions/:id/dashboard
// @Summary Thống kê ca thi (Admin)
// @Description Xem thống kê nhanh (số ghế trống, số đăng ký...) của ca thi.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/:id/dashboard [get]
func (h *Handler) GetSessionDashboard(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	d, err := h.svc.GetSessionDashboard(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, d)
}

// GetPaymentReconcile GET /admin/payments/reconcile?date=YYYY-MM-DD
func (h *Handler) GetPaymentReconcile(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		utils.BadRequest(c, "Tham số date là bắt buộc (định dạng YYYY-MM-DD)")
		return
	}
	list, err := h.svc.GetPaymentReconcile(c.Request.Context(), date)
	if err != nil {
		utils.InternalServerError(c, "Lấy báo cáo đối soát thất bại")
		return
	}
	utils.OK(c, list)
}
