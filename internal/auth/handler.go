package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the auth Service.
type Handler struct{ svc *Service }

// NewHandler creates an auth Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register POST /auth/register
func (h *Handler) Register(c *gin.Context) {
	var req request.CreateAccountRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, acc)
}

// Login POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var req request.LoginRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	tokens, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		utils.Unauthorized(c, err.Error())
		return
	}
	utils.OK(c, tokens)
}

// Me GET /me
func (h *Handler) Me(c *gin.Context) {
	acc, err := h.svc.GetAccount(c.Request.Context(), mustAccountID(c))
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, acc)
}

// UpdateMe PUT /me
func (h *Handler) UpdateMe(c *gin.Context) {
	var req request.UpdateAccountRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, err := h.svc.UpdateAccount(c.Request.Context(), mustAccountID(c), req)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, acc)
}

// DeleteMe DELETE /me
func (h *Handler) DeleteMe(c *gin.Context) {
	if err := h.svc.SoftDeleteAccount(c.Request.Context(), mustAccountID(c)); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "account deleted"})
}

func mustAccountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}
