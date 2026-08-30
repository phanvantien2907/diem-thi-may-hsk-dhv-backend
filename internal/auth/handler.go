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
	var req request.RegisterRequestDTO
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
		if err.Error() == "tài khoản đã tồn tại" {
			utils.Conflict(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, gin.H{
		"account": acc,
		"message": "Tài khoản đã được tạo thành công.",
	})
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
	ip := c.ClientIP()
	device := c.GetHeader("User-Agent")

	tokens, err := h.svc.Login(c.Request.Context(), req, ip, device)
	if err != nil {
		utils.Unauthorized(c, err.Error())
		return
	}
	utils.OK(c, tokens)
}

// RefreshToken POST /auth/refresh
func (h *Handler) RefreshToken(c *gin.Context) {
	var req request.RefreshTokenRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	tokens, err := h.svc.RefreshToken(c.Request.Context(), req)
	if err != nil {
		utils.Unauthorized(c, err.Error())
		return
	}
	utils.OK(c, tokens)
}

// ChangePassword PATCH /me/password
func (h *Handler) ChangePassword(c *gin.Context) {
	var req request.ChangePasswordRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.ChangePassword(c.Request.Context(), utils.MustAccountID(c), req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đổi mật khẩu thành công"})
}

// ForceChangePassword POST /auth/force-password-change
func (h *Handler) ForceChangePassword(c *gin.Context) {
	var req request.ForceChangePasswordRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.ForceChangePassword(c.Request.Context(), req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đổi mật khẩu thành công, vui lòng đăng nhập lại"})
}

// ListSessions GET /me/sessions
func (h *Handler) ListSessions(c *gin.Context) {
	sessions, err := h.svc.ListSessions(c.Request.Context(), utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, sessions)
}

// DeleteSession DELETE /me/sessions/:id
func (h *Handler) DeleteSession(c *gin.Context) {
	sessionID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID phiên đăng nhập không hợp lệ")
		return
	}
	if err := h.svc.Logout(c.Request.Context(), utils.MustAccountID(c), sessionID); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đã đăng xuất khỏi thiết bị"})
}

// Logout POST /auth/logout
func (h *Handler) Logout(c *gin.Context) {
	var body struct {
		SessionID int64 `json:"session_id" validate:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(body); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.Logout(c.Request.Context(), utils.MustAccountID(c), body.SessionID); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đăng xuất thành công"})
}

// LogoutAll POST /auth/logout-all
func (h *Handler) LogoutAll(c *gin.Context) {
	if err := h.svc.LogoutAll(c.Request.Context(), utils.MustAccountID(c)); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đã đăng xuất khỏi tất cả thiết bị"})
}

// Me GET /me
func (h *Handler) Me(c *gin.Context) {
	acc, err := h.svc.GetAccount(c.Request.Context(), utils.MustAccountID(c))
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
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, err := h.svc.UpdateAccount(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, acc)
}

// DeleteMe DELETE /me
func (h *Handler) DeleteMe(c *gin.Context) {
	if err := h.svc.SoftDeleteAccountMe(c.Request.Context(), utils.MustAccountID(c)); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa tài khoản"})
}

// AdminDeleteAccount DELETE /admin/delete-account/:id
func (h *Handler) AdminDeleteAccount(c *gin.Context) {
	targetID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID tài khoản không hợp lệ")
		return
	}
	if err := h.svc.SoftDeleteAccountByAdmin(c.Request.Context(), utils.MustAccountID(c), targetID); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa tài khoản"})
}

// CreateStaffAccount POST /admin/staff-accounts
func (h *Handler) CreateStaffAccount(c *gin.Context) {
	var req request.CreateStaffRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	acc, tempPass, err := h.svc.CreateStaffAccount(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		if err.Error() == "tài khoản đã tồn tại" {
			utils.Conflict(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	// Return temp password so admin can deliver it to the staff member
	utils.Created(c, gin.H{
		"account":       acc,
		"temp_password": tempPass,
		"message":       "Tài khoản nhân viên đã được tạo. Nhân viên phải đổi mật khẩu khi đăng nhập lần đầu.",
	})
}
