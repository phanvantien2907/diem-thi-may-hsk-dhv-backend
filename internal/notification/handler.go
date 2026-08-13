package notification

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the notification Service.
type Handler struct{ svc *Service }

// NewHandler creates a notification Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetMyNotifications GET /notifications
func (h *Handler) GetMyNotifications(c *gin.Context) {
	id := accountID(c)
	list, err := h.svc.GetMyNotifications(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "failed to fetch notifications")
		return
	}
	utils.OK(c, list)
}

// MarkRead PUT /notifications/:id/read
func (h *Handler) MarkRead(c *gin.Context) {
	nid, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid notification id")
		return
	}
	if err := h.svc.MarkSent(c.Request.Context(), nid); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "notification marked as read"})
}

func accountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
