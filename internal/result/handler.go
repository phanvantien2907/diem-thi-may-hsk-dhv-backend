package result

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/result/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the result Service.
type Handler struct{ svc *Service }

// NewHandler creates a result Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateResult POST /admin/results
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
	r, err := h.svc.CreateResult(c.Request.Context(), req, accountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}

// GetResult GET /results/:registration_id
func (h *Handler) GetResult(c *gin.Context) {
	id, ok := paramID(c, "registration_id")
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
	r, err := h.svc.CreateRecheckRequest(c.Request.Context(), req, accountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}

func accountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
