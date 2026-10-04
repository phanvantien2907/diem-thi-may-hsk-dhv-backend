package dashboard

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/dashboard/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the dashboard Service.
type Handler struct{ svc *Service }

// NewHandler creates a dashboard Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ─── 1. GET /admin/metrics/overview ─────────────────────────────────────────

// GetMetricsOverview godoc
// @Summary      Lấy chỉ số tổng quan
// @Description  Lấy các chỉ số KPI: doanh thu, tổng số đăng ký, tỷ lệ lấp đầy, tỷ lệ thanh toán thành công
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/metrics/overview [get]
func (h *Handler) GetMetricsOverview(c *gin.Context) {
	data, err := h.svc.GetMetricsOverview(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy chỉ số tổng quan thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 2. GET /admin/metrics/revenue-chart ────────────────────────────────────

// GetRevenueChart godoc
// @Summary      Lấy biểu đồ doanh thu
// @Description  Lấy dữ liệu vẽ biểu đồ doanh thu và lượng đăng ký theo ngày (7 ngày hoặc 30 ngày)
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        range query string true "Khoảng thời gian (7days hoặc 30days)"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/metrics/revenue-chart [get]
func (h *Handler) GetRevenueChart(c *gin.Context) {
	var req request.RevenueChartRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số range là bắt buộc (7days hoặc 30days)")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, "Tham số range không hợp lệ, chỉ chấp nhận 7days hoặc 30days")
		return
	}
	data, err := h.svc.GetRevenueChart(c.Request.Context(), req.Range)
	if err != nil {
		utils.InternalServerError(c, "Lấy biểu đồ doanh thu thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 3. GET /admin/metrics/hsk-distribution ─────────────────────────────────

// GetHSKDistribution godoc
// @Summary      Lấy phân bố chứng chỉ HSK
// @Description  Lấy tỷ lệ thí sinh đăng ký theo từng cấp độ HSK (chỉ tính các đơn đã confirmed)
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/metrics/hsk-distribution [get]
func (h *Handler) GetHSKDistribution(c *gin.Context) {
	data, err := h.svc.GetHSKDistribution(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy phân bố HSK thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 4. GET /admin/metrics/payment-funnel ───────────────────────────────────

// GetPaymentFunnel godoc
// @Summary      Lấy phễu thanh toán trong ngày
// @Description  Phân tích lượng giao dịch thành công / đang chờ / thất bại theo từng khung giờ trong ngày hôm nay
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/metrics/payment-funnel [get]
func (h *Handler) GetPaymentFunnel(c *gin.Context) {
	data, err := h.svc.GetPaymentFunnel(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy phễu thanh toán thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 5. GET /admin/sessions/live-status ─────────────────────────────────────

// GetLiveSessionStatus godoc
// @Summary      Giám sát trạng thái ca thi trực tiếp
// @Description  Xem trực tiếp số ghế trống, số ghế đang bị giữ và số ghế đã chốt của các ca thi đang mở
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/sessions/live-status [get]
func (h *Handler) GetLiveSessionStatus(c *gin.Context) {
	data, err := h.svc.GetLiveSessionStatus(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy trạng thái ca thi thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 6. GET /admin/transactions/exceptions ──────────────────────────────────

// GetTransactionExceptions godoc
// @Summary      Danh sách giao dịch ngoại lệ
// @Description  Xem các giao dịch thanh toán bị lỗi, sai tiền hoặc quá hạn chưa xử lý
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "Trang hiện tại (mặc định 1)"
// @Param        limit query int false "Số lượng bản ghi mỗi trang (mặc định 20)"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/transactions/exceptions [get]
func (h *Handler) GetTransactionExceptions(c *gin.Context) {
	var req request.ExceptionListRequestDTO
	_ = c.ShouldBindQuery(&req)

	items, meta, err := h.svc.GetTransactionExceptions(c.Request.Context(), req.Page, req.Limit)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách giao dịch ngoại lệ thất bại")
		return
	}
	utils.OKWithMeta(c, items, meta)
}

// ─── 7. POST /admin/transactions/:id/manual-approve ─────────────────────────

// ManualApprove godoc
// @Summary      Duyệt giao dịch thủ công
// @Description  Cho phép Admin duyệt thủ công một giao dịch bị lỗi (force approve), hệ thống sẽ chốt đơn và giữ ghế
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "ID của Giao dịch (Payment ID)"
// @Param        body body request.ManualApproveRequestDTO true "Lý do duyệt thủ công"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/transactions/{id}/manual-approve [post]
func (h *Handler) ManualApprove(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.ManualApproveRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	adminID := utils.MustAccountID(c)
	data, err := h.svc.ManualApprove(c.Request.Context(), id, adminID, req.Reason)
	if err != nil {
		if IsBusinessError(err) {
			errMsg := err.Error()
			if errMsg == "Không tìm thấy giao dịch" {
				utils.NotFound(c, errMsg)
				return
			}
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Duyệt thủ công thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 8. POST /admin/transactions/:id/refund-mark ────────────────────────────

// RefundMark godoc
// @Summary      Đánh dấu đã hoàn tiền
// @Description  Cho phép Admin đánh dấu một giao dịch là đã hoàn tiền cho thí sinh
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "ID của Giao dịch (Payment ID)"
// @Param        body body request.RefundMarkRequestDTO true "Lý do hoàn tiền"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/transactions/{id}/refund-mark [post]
func (h *Handler) RefundMark(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "Tham số id không hợp lệ")
		return
	}
	var req request.RefundMarkRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	adminID := utils.MustAccountID(c)
	data, err := h.svc.RefundMark(c.Request.Context(), id, adminID, req.Reason)
	if err != nil {
		if IsBusinessError(err) {
			errMsg := err.Error()
			if errMsg == "Không tìm thấy giao dịch" {
				utils.NotFound(c, errMsg)
				return
			}
			utils.Conflict(c, errMsg)
			return
		}
		utils.InternalServerError(c, "Đánh dấu hoàn tiền thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 9. GET /admin/logs/recent ──────────────────────────────────────────────

// GetRecentLogs godoc
// @Summary      Lịch sử hoạt động gần đây
// @Description  Xem danh sách các hành động Audit Log mới nhất trên hệ thống (của tất cả Admin)
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "Trang hiện tại (mặc định 1)"
// @Param        limit query int false "Số lượng bản ghi mỗi trang (mặc định 20)"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/logs/recent [get]
func (h *Handler) GetRecentLogs(c *gin.Context) {
	var req request.RecentLogsRequestDTO
	_ = c.ShouldBindQuery(&req)

	items, meta, err := h.svc.GetRecentLogs(c.Request.Context(), req.Page, req.Limit)
	if err != nil {
		utils.InternalServerError(c, "Lấy nhật ký hoạt động thất bại")
		return
	}
	utils.OKWithMeta(c, items, meta)
}

// ─── 10. POST /admin/system/clear-cache ─────────────────────────────────────

// ClearCache godoc
// @Summary      Xoá bộ nhớ đệm (Cache) Dashboard
// @Description  Xoá bộ nhớ đệm In-memory của Dashboard để ép hệ thống query lại dữ liệu mới nhất
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/system/clear-cache [post]
func (h *Handler) ClearCache(c *gin.Context) {
	adminID := utils.MustAccountID(c)
	data, err := h.svc.ClearCache(c.Request.Context(), adminID)
	if err != nil {
		utils.InternalServerError(c, "Xóa bộ nhớ đệm thất bại")
		return
	}
	utils.OK(c, data)
}

// ─── 11. GET /admin/users/me/audit-log ──────────────────────────────────────

// GetMyAuditLog godoc
// @Summary      Lịch sử thao tác cá nhân
// @Description  Xem danh sách các hành động Audit Log do chính Admin đang đăng nhập thực hiện
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "Trang hiện tại (mặc định 1)"
// @Param        limit query int false "Số lượng bản ghi mỗi trang (mặc định 20)"
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /admin/users/me/audit-log [get]
func (h *Handler) GetMyAuditLog(c *gin.Context) {
	adminID := utils.MustAccountID(c)
	var req request.AuditLogMeRequestDTO
	_ = c.ShouldBindQuery(&req)

	items, meta, err := h.svc.GetMyAuditLog(c.Request.Context(), adminID, req.Page, req.Limit)
	if err != nil {
		utils.InternalServerError(c, "Lấy lịch sử thao tác thất bại")
		return
	}
	utils.OKWithMeta(c, items, meta)
}
