package location

import (
	"github.com/gin-gonic/gin"
	locreq "github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the location Service.
type Handler struct{ svc *Service }

// NewHandler creates a location Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListProvinces GET /locations/provinces
// @Summary Danh sách Tỉnh/Thành phố
// @Description Lấy dữ liệu Tỉnh/Thành phố.
// @Tags Location
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /locations/provinces [get]
func (h *Handler) ListProvinces(c *gin.Context) {
	list, err := h.svc.ListProvinces(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách tỉnh/thành phố thất bại")
		return
	}
	utils.OK(c, list)
}

// ListWards GET /locations/provinces/:id/wards
func (h *Handler) ListWards(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID tỉnh/thành phố không hợp lệ")
		return
	}
	list, err := h.svc.ListWards(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách xã/phường thất bại")
		return
	}
	utils.OK(c, list)
}

// GetWardDeliveryCoverage GET /locations/wards/:id/delivery-coverage?courier=ghn
// @Summary Kiểm tra tuyến giao hàng
// @Description Kiểm tra xem đơn vị vận chuyển có hỗ trợ giao tới xã/phường này không.
// @Tags Location
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /locations/wards/:id/delivery-coverage [get]
func (h *Handler) GetWardDeliveryCoverage(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID xã/phường không hợp lệ")
		return
	}
	courier := c.Query("courier") // optional: "ghn" or "ghtk"

	e, err := h.svc.GetWardDeliveryCoverage(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, ToWardCoverageResponseDTO(e, courier))
}

// StatsCandidatesByProvince GET /admin/locations/stats/candidates-by-province
// @Summary Thống kê theo Tỉnh (Admin)
// @Description Thống kê số lượng thí sinh theo khu vực địa lý.
// @Tags Location
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/locations/stats/candidates-by-province [get]
func (h *Handler) StatsCandidatesByProvince(c *gin.Context) {
	list, err := h.svc.StatsCandidatesByProvince(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy thống kê thí sinh thất bại")
		return
	}
	utils.OK(c, list)
}

// MergeWard POST /admin/locations/wards/:id/merge
func (h *Handler) MergeWard(c *gin.Context) {
	sourceID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID xã/phường không hợp lệ")
		return
	}

	var req locreq.MergeWardRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	actorID := utils.MustAccountID(c)
	if err := h.svc.MergeWard(c.Request.Context(), actorID, sourceID, req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "xã/phường đã được gộp thành công"})
}
