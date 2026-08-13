package location

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the location Service.
type Handler struct{ svc *Service }

// NewHandler creates a location Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListProvinces GET /provinces
func (h *Handler) ListProvinces(c *gin.Context) {
	list, err := h.svc.ListProvinces(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "failed to fetch provinces")
		return
	}
	utils.OK(c, list)
}

// ListWards GET /provinces/:id/wards
func (h *Handler) ListWards(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid province id")
		return
	}
	list, err := h.svc.ListWards(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "failed to fetch wards")
		return
	}
	utils.OK(c, list)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
