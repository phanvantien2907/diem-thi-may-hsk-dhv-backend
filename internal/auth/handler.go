package auth

import (
	"strconv"

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
		if err.Error() == "account already exists" {
			utils.Conflict(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to register account")
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
		if err.Error() == "invalid credentials" {
			utils.Unauthorized(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to login")
		return
	}
	utils.OK(c, tokens)
}

// Me GET /me
func (h *Handler) Me(c *gin.Context) {
	acc, err := h.svc.GetAccount(c.Request.Context(), mustAccountID(c))
	if err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to get account")
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
		if err.Error() == "account not found" {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to update account")
		return
	}
	utils.OK(c, acc)
}

// DeleteMe DELETE /me
func (h *Handler) DeleteMe(c *gin.Context) {
	if err := h.svc.SoftDeleteAccountMe(c.Request.Context(), mustAccountID(c)); err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to delete account")
		return
	}
	utils.OK(c, gin.H{"message": "account deleted"})
}

// AdminDeleteAccount DELETE /auth/delete-account/:id
func (h *Handler) AdminDeleteAccount(c *gin.Context) {
	adminID := mustAccountID(c)
	targetID, _ := paramID(c, "id")
	if err := h.svc.SoftDeleteAccountByAdmin(c.Request.Context(), adminID, targetID); err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "failed to delete account")
		return
	}
	utils.OK(c, gin.H{"message": "account deleted"})
}

// mustAccountID get account ID from context.
func mustAccountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}

// Parse string to int from params.
func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
