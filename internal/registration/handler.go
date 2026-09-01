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
