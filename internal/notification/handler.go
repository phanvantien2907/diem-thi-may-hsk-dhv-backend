package notification

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the notification Service.
type Handler struct{ svc *Service }

// NewHandler creates a notification Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetMyNotifications GET /notifications
// @Summary Danh sách thông báo
// @Description Lấy danh sách thông báo hệ thống của người dùng hiện tại. Hỗ trợ tham số after_id để đồng bộ các thông báo bị lỡ khi kết nối lại WebSocket.
// @Tags Notification
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param after_id query int false "Chỉ lấy các thông báo có ID lớn hơn after_id"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /notifications [get]
func (h *Handler) GetMyNotifications(c *gin.Context) {
	id := accountID(c)
	var afterID int64
	if rawAfter := strings.TrimSpace(c.Query("after_id")); rawAfter != "" {
		if parsed, err := strconv.ParseInt(rawAfter, 10, 64); err == nil && parsed > 0 {
			afterID = parsed
		}
	}
	list, err := h.svc.GetMyNotifications(c.Request.Context(), id, afterID)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách thông báo thất bại")
		return
	}
	utils.OK(c, list)
}

// MarkRead PATCH /notifications/:id/read (also supports PUT /notifications/:id/read)
// @Summary Đánh dấu thông báo đã đọc
// @Description Đánh dấu một thông báo của người dùng hiện tại sang trạng thái đã đọc (is_read = true).
// @Tags Notification
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID thông báo"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "ID thông báo không hợp lệ"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy thông báo"
// @Router /notifications/{id}/read [patch]
func (h *Handler) MarkRead(c *gin.Context) {
	nid, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID thông báo không hợp lệ")
		return
	}
	uid := accountID(c)
	if err := h.svc.MarkRead(c.Request.Context(), nid, uid); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, gin.H{
		"id":      nid,
		"is_read": true,
		"message": "Đã đánh dấu thông báo là đã đọc",
	})
}

func accountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	id, _ := v.(int64)
	return id
}
