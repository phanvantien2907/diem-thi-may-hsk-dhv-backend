package registration

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the registration Service.
type Handler struct{ svc *Service }

// NewHandler creates a registration Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) getCandidateID(c *gin.Context) (int64, bool) {
	accountID := utils.MustAccountID(c)
	candidateID, err := h.svc.GetCandidateID(c.Request.Context(), accountID)
	if err != nil {
		utils.Forbidden(c, err.Error())
		return 0, false
	}
	return candidateID, true
}

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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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
	candidateID, ok := h.getCandidateID(c)
	if !ok {
		return
	}
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

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Registration Results (Kết quả Đăng ký theo Đợt thi & Ca thi)
// ─────────────────────────────────────────────────────────────────────────────

// ListPeriodRegistrationResults GET /admin/registration-results/periods
// @Summary Danh sách kết quả đăng ký theo đợt thi (Admin)
// @Description Xem danh sách các đợt thi (batch) kèm thống kê các ca thi, số lượng đăng ký, cảnh báo vận hành và đối soát lệch ghế.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int false "Trang (mặc định 1)"
// @Param limit query int false "Số lượng mỗi trang (mặc định 20, tối đa 100)"
// @Param status query string false "Lọc theo trạng thái đợt thi"
// @Param search query string false "Tìm theo mã hoặc tên đợt thi"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/periods [get]
func (h *Handler) ListPeriodRegistrationResults(c *gin.Context) {
	var req request.AdminListPeriodResultsRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	list, meta, err := h.svc.ListPeriodRegistrationResults(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, "Không thể tải danh sách kết quả đăng ký theo đợt thi")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// GetPeriodRegistrationResults GET /admin/registration-results/periods/:period_id
// @Summary Chi tiết kết quả đăng ký của một đợt thi (Admin)
// @Description Xem chi tiết đợt thi cùng toàn bộ các ca thi thuộc đợt, thống kê số lượng, cảnh báo và kiểm tra sai lệch dữ liệu.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param period_id path int true "ID đợt thi"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "ID đợt thi không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/periods/{period_id} [get]
func (h *Handler) GetPeriodRegistrationResults(c *gin.Context) {
	periodID, ok := utils.ParamID(c, "period_id")
	if !ok {
		utils.BadRequest(c, "ID đợt thi không hợp lệ")
		return
	}
	res, err := h.svc.GetPeriodRegistrationResults(c.Request.Context(), periodID)
	if err != nil {
		if strings.Contains(err.Error(), "Không tìm thấy") {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Không thể tải kết quả đăng ký của đợt thi")
		return
	}
	utils.OK(c, res)
}

// GetSlotRegistrationSummary GET /admin/registration-results/sessions/:session_id/summary
// @Summary Thống kê thời gian thực và cảnh báo của một ca thi (Admin)
// @Description Xem số lượng đăng ký thời gian thực, tỷ lệ lấp đầy, phân loại đủ điều kiện, cảnh báo vận hành và kiểm tra lệch số ghế của ca thi.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param session_id path int true "ID ca thi"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "ID ca thi không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/sessions/{session_id}/summary [get]
func (h *Handler) GetSlotRegistrationSummary(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "session_id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	res, err := h.svc.GetSlotRegistrationSummary(c.Request.Context(), sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "Không tìm thấy") {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Không thể tải thống kê ca thi")
		return
	}
	utils.OK(c, res)
}

// ListSlotRegistrations GET /admin/registration-results/sessions/:session_id/registrations
// @Summary Danh sách thí sinh đăng ký trong ca thi (Admin)
// @Description Xem danh sách thí sinh của ca thi (có phân trang, che PII theo Nghị định 13/2023, nhận diện đăng ký 1 hay nhiều ca, lọc đủ/không đủ điều kiện và cung cấp shortcut duyệt hồ sơ).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param session_id path int true "ID ca thi"
// @Param page query int false "Trang (mặc định 1)"
// @Param limit query int false "Số lượng mỗi trang (mặc định 20, tối đa 100)"
// @Param search query string false "Tìm theo họ tên, email, SĐT, CCCD, số ghế"
// @Param registration_status query string false "Lọc trạng thái ĐK: pending_payment, confirmed, cancelled, all"
// @Param verification_status query string false "Lọc trạng thái hồ sơ: pending, verified, rejected, missing, all"
// @Param eligibility query string false "Lọc điều kiện: eligible, ineligible, pending_review, all"
// @Param slot_scope query string false "Lọc số ca đăng ký: single (1 ca), multiple (nhiều ca), all"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Tham số lọc không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/sessions/{session_id}/registrations [get]
func (h *Handler) ListSlotRegistrations(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "session_id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req request.AdminListSlotRegistrationsRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	list, meta, err := h.svc.ListSlotRegistrations(c.Request.Context(), sessionID, req)
	if err != nil {
		if strings.Contains(err.Error(), "không hợp lệ") {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Không thể tải danh sách thí sinh đăng ký")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminListWaitlist GET /admin/registration-results/sessions/:session_id/waitlist
// @Summary Xem danh sách chờ của ca thi (Admin)
// @Description Xem danh sách thí sinh đang chờ chỗ trống trong ca thi và vị trí xếp hàng (queue_position).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param session_id path int true "ID ca thi"
// @Param page query int false "Trang (mặc định 1)"
// @Param limit query int false "Số lượng mỗi trang (mặc định 20, tối đa 100)"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "ID ca thi không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/sessions/{session_id}/waitlist [get]
func (h *Handler) AdminListWaitlist(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "session_id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req request.AdminListWaitlistRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	list, meta, err := h.svc.AdminListWaitlist(c.Request.Context(), sessionID, req)
	if err != nil {
		utils.InternalServerError(c, "Không thể tải danh sách chờ của ca thi")
		return
	}
	utils.OKWithMeta(c, list, meta)
}

// AdminCheckDiscrepancies GET /admin/registration-results/discrepancies
// @Summary Kiểm tra sai lệch dữ liệu đăng ký và số ghế (Admin)
// @Description Phát hiện các ca thi có số đơn confirmed không khớp số ghế booked, đơn pending_payment không khớp ghế held, hoặc đơn confirmed chưa thanh toán.
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param period_id query int false "Lọc theo ID đợt thi"
// @Param session_id query int false "Lọc theo ID ca thi"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Tham số không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/discrepancies [get]
func (h *Handler) AdminCheckDiscrepancies(c *gin.Context) {
	var periodID, sessionID int64
	if raw := strings.TrimSpace(c.Query("period_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			utils.BadRequest(c, "Tham số period_id không hợp lệ")
			return
		}
		periodID = v
	}
	if raw := strings.TrimSpace(c.Query("session_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			utils.BadRequest(c, "Tham số session_id không hợp lệ")
			return
		}
		sessionID = v
	}
	list, err := h.svc.AdminCheckDiscrepancies(c.Request.Context(), periodID, sessionID)
	if err != nil {
		utils.InternalServerError(c, "Không thể kiểm tra sai lệch dữ liệu")
		return
	}
	utils.OK(c, list)
}

// AdminExportRegistrations GET /admin/registration-results/export
// @Summary Xuất danh sách kết quả đăng ký ra PDF hoặc Excel/CSV (Admin)
// @Description Xuất danh sách thí sinh lọc theo đợt thi hoặc ca thi và tình trạng đủ điều kiện sang tệp PDF hoặc Excel (CSV UTF-8 BOM).
// @Tags Registration
// @Produce application/octet-stream
// @Security BearerAuth
// @Param period_id query int false "ID đợt thi"
// @Param session_id query int false "ID ca thi"
// @Param format query string true "Định dạng xuất: pdf, excel, csv"
// @Param eligibility query string false "Lọc điều kiện: eligible, ineligible, pending_review, all"
// @Param registration_status query string false "Lọc trạng thái ĐK: confirmed, pending_payment, cancelled, all"
// @Param slot_scope query string false "Lọc số ca đăng ký: single, multiple, all"
// @Param search query string false "Tìm theo tên, CCCD, SĐT, email"
// @Success 200 {file} binary "Tệp PDF hoặc CSV"
// @Failure 400 {object} map[string]interface{} "Tham số không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/registration-results/export [get]
func (h *Handler) AdminExportRegistrations(c *gin.Context) {
	var req request.AdminExportRegistrationsRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, "Tham số định dạng xuất (format) là bắt buộc (pdf, excel, xlsx, json, txt hoặc csv)")
		return
	}
	adminID := utils.MustAccountID(c)

	err := h.svc.AdminStreamExportRegistrations(c.Request.Context(), adminID, req, func(filename, contentType string) error {
		c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
		return nil
	}, c.Writer)

	if err != nil {
		if errors.Is(err, ErrNoExportData) || strings.Contains(err.Error(), "Không có dữ liệu") {
			utils.BadRequest(c, "Không có dữ liệu đăng ký phù hợp với bộ lọc đã chọn")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			utils.InternalServerError(c, "Yêu cầu xuất dữ liệu đã quá thời gian chờ (timeout)")
			return
		}
		if errors.Is(err, context.Canceled) {
			// Client disconnected prematurely; nothing more to write
			return
		}
		if strings.Contains(err.Error(), "Bắt buộc") || strings.Contains(err.Error(), "không hợp lệ") {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Không thể xuất danh sách kết quả đăng ký")
		return
	}
}

// AdminCancelRegistration POST /admin/registrations/:id/cancel
// @Summary Admin hủy đăng ký thi (nhả ghế & kích hoạt hoàn tiền)
// @Description Hủy phiếu đăng ký thi của thí sinh, giải phóng ghế về available và tự động chuyển giao dịch đã thanh toán sang luồng hoàn tiền (pending_refund).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID phiếu đăng ký"
// @Param request body request.AdminCancelRegistrationRequestDTO true "Lý do hủy và ảnh bằng chứng (nếu có)"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy phiếu đăng ký"
// @Failure 409 {object} map[string]interface{} "Không thể hủy phiếu đăng ký"
// @Router /admin/registrations/{id}/cancel [post]
func (h *Handler) AdminCancelRegistration(c *gin.Context) {
	regID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	var req request.AdminCancelRegistrationRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, "Lý do hủy đăng ký phải có ít nhất 3 ký tự")
		return
	}
	adminID := utils.MustAccountID(c)
	res, err := h.svc.AdminCancelRegistration(c.Request.Context(), regID, adminID, req)
	if err != nil {
		if strings.Contains(err.Error(), "Không tìm thấy") {
			utils.NotFound(c, err.Error())
			return
		}
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminTransferRegistration POST /admin/registrations/:id/transfer
// @Summary Admin chuyển ca thi cho thí sinh
// @Description Chuyển thí sinh sang ca thi khác bằng hàm fn_transfer_registration hiện có (giữ nguyên trạng thái thanh toán, nhả ghế cũ và cấp ghế mới).
// @Tags Registration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID phiếu đăng ký"
// @Param request body request.AdminTransferRegistrationRequestDTO true "ID ca thi đích và lý do chuyển"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy phiếu đăng ký"
// @Failure 409 {object} map[string]interface{} "Không thể chuyển ca thi"
// @Router /admin/registrations/{id}/transfer [post]
func (h *Handler) AdminTransferRegistration(c *gin.Context) {
	regID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiếu đăng ký không hợp lệ")
		return
	}
	var req request.AdminTransferRegistrationRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, "ID ca thi đích (new_exam_session_id) không hợp lệ")
		return
	}
	adminID := utils.MustAccountID(c)
	res, err := h.svc.AdminTransferRegistration(c.Request.Context(), regID, adminID, req)
	if err != nil {
		if strings.Contains(err.Error(), "Không tìm thấy") {
			utils.NotFound(c, err.Error())
			return
		}
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, res)
}
