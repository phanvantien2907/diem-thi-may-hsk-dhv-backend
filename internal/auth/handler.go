package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/auth/dto/request"
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
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "account already exists" {
			utils.Conflict(c, "Tài khoản đã tồn tại")
			return
		}
		utils.InternalServerError(c, "Đăng ký tài khoản thất bại")
		return
	}
	utils.Created(c, acc)
}

// Login POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var req request.LoginRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	tokens, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "invalid credentials" {
			utils.Unauthorized(c, "Thông tin đăng nhập không chính xác")
			return
		}
		utils.InternalServerError(c, "Đăng nhập thất bại")
		return
	}
	utils.OK(c, tokens)
}

// Me GET /me
func (h *Handler) Me(c *gin.Context) {
	userID := utils.MustAccountID(c)
	acc, err := h.svc.GetAccount(c.Request.Context(), userID)
	if err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, "Không tìm thấy tài khoản")
			return
		}
		utils.InternalServerError(c, "Lấy thông tin tài khoản thất bại")
		return
	}
	utils.OK(c, acc)
}

// UpdateMe PUT /me
func (h *Handler) UpdateMe(c *gin.Context) {
	userID := utils.MustAccountID(c)
	var req request.UpdateAccountRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, err := h.svc.UpdateAccount(c.Request.Context(), userID, req)
	if err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, "Không tìm thấy tài khoản")
			return
		}
		utils.InternalServerError(c, "Cập nhật tài khoản thất bại")
		return
	}
	utils.OK(c, acc)
}

// DeleteMe DELETE /me
func (h *Handler) DeleteMe(c *gin.Context) {
	userID := utils.MustAccountID(c)
	if err := h.svc.SoftDeleteAccountMe(c.Request.Context(), userID); err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, "Không tìm thấy tài khoản")
			return
		}
		utils.InternalServerError(c, "Xóa tài khoản thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa tài khoản"})
}

// AdminDeleteAccount DELETE /auth/delete-account/:id
func (h *Handler) AdminDeleteAccount(c *gin.Context) {
	adminID := utils.MustAccountID(c)
	targetID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID tài khoản không hợp lệ")
		return
	}
	if err := h.svc.SoftDeleteAccountByAdmin(c.Request.Context(), adminID, targetID); err != nil {
		if err.Error() == "account not found" {
			utils.NotFound(c, "Không tìm thấy tài khoản")
			return
		}
		utils.InternalServerError(c, "Xóa tài khoản thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa tài khoản"})
}
