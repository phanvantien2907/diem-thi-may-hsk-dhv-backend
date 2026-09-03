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
// @Summary Đăng ký tài khoản
// @Description Tạo một tài khoản thí sinh mới trong hệ thống.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body request.RegisterRequestDTO true "Dữ liệu yêu cầu"
// @Success 201 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 409 {object} map[string]interface{} "Tài khoản đã tồn tại"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/register [post]
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
// @Summary Đăng nhập
// @Description Xác thực người dùng và trả về JWT Tokens (Access/Refresh).
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body request.LoginRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Thông tin đăng nhập không chính xác"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/login [post]
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
// @Summary Làm mới Token
// @Description Sử dụng Refresh Token để cấp lại Access Token mới khi hết hạn.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.RefreshTokenRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 401 {object} map[string]interface{} "Token không hợp lệ hoặc đã hết hạn"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/refresh [post]
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
// @Summary Đổi mật khẩu
// @Description Thay đổi mật khẩu của người dùng đang đăng nhập.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.ChangePasswordRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/password [patch]
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
// @Summary Bắt buộc đổi mật khẩu
// @Description Sử dụng cho nhân viên đổi mật khẩu tạm thời lần đầu đăng nhập.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.ForceChangePasswordRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/force-password-change [post]
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
// @Summary Danh sách phiên đăng nhập
// @Description Lấy danh sách các phiên đăng nhập (thiết bị) hiện tại của người dùng.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/sessions [get]
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
// @Summary Đăng xuất (1 phiên)
// @Description Đăng xuất khỏi thiết bị hiện tại bằng session_id.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/logout [post]
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
// @Summary Đăng xuất (tất cả)
// @Description Đăng xuất khỏi toàn bộ các thiết bị (xóa tất cả session).
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /auth/logout-all [post]
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
// @Summary Cập nhật thông tin cá nhân
// @Description Cập nhật các thông tin cơ bản của tài khoản đang đăng nhập.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.UpdateAccountRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me [put]
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
// @Summary Xóa tài khoản cá nhân
// @Description Yêu cầu vô hiệu hóa (soft-delete) tài khoản của chính mình.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me [delete]
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
// @Summary Tạo tài khoản nhân viên
// @Description Admin tạo tài khoản cho nhân viên/cán bộ. Trả về mật khẩu tạm.
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.CreateStaffRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 409 {object} map[string]interface{} "Tài khoản đã tồn tại"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/staff-accounts [post]
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
