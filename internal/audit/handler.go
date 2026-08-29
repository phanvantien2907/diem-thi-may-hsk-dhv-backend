package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/audit/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the audit Service.
type Handler struct{ svc Service }

// NewHandler creates an audit Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List GET /admin/audit-logs
// Accepts query params: actor_id, action, entity_table, entity_id, from, to, page, limit.
func (h *Handler) List(c *gin.Context) {
	var req request.ListAuditLogRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ: "+err.Error())
		return
	}
	result, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, "Lấy lịch sử hoạt động thất bại")
		return
	}
	utils.OKWithMeta(c, result.Items, &result.Meta)
}
