package result

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/result/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the result Service.
type Handler struct{ svc *Service }

// NewHandler creates a result Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateResult POST /admin/results
// @Summary Nhập điểm (Admin)
// @Description Admin nhập/cập nhật điểm thi cho thí sinh.
// @Tags Result
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.CreateResultRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/results [post]
func (h *Handler) CreateResult(c *gin.Context) {
	var req request.CreateResultRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	r, err := h.svc.CreateResult(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}

// GetResult GET /results/:registration_id
// @Summary Xem điểm thi
// @Description Thí sinh tra cứu điểm thi của mình.
// @Tags Result
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param registration_id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Chưa có điểm"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /results/:registration_id [get]
func (h *Handler) GetResult(c *gin.Context) {
	id, ok := utils.ParamID(c, "registration_id")
	if !ok {
		utils.BadRequest(c, "invalid registration_id")
		return
	}
	r, err := h.svc.GetResultByRegistration(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, r)
}

// CreateRecheck POST /rechecks
func (h *Handler) CreateRecheck(c *gin.Context) {
	var req request.CreateRecheckRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	r, err := h.svc.CreateRecheckRequest(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}
