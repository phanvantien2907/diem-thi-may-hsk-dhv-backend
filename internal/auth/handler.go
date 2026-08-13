package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the auth Service.
type Handler struct{ svc *Service }

// NewHandler creates an auth Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register POST /auth/register
func (h *Handler) Register(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Phone    string `json:"phone"`
		RoleCode string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("username", body.Username).MinLen("username", body.Username, 3)
	v.Required("email", body.Email).Email("email", body.Email)
	v.Required("password", body.Password).MinLen("password", body.Password, 8)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	if body.RoleCode == "" {
		body.RoleCode = "candidate"
	}
	acc, err := h.svc.Register(c.Request.Context(), RegisterInput{
		Username: body.Username, Email: body.Email,
		Password: body.Password, Phone: body.Phone, RoleCode: body.RoleCode,
	})
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, acc)
}

// Login POST /auth/login
func (h *Handler) Login(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("username", body.Username).Required("password", body.Password)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	tokens, err := h.svc.Login(c.Request.Context(), LoginInput{
		Username: body.Username, Password: body.Password,
	})
	if err != nil {
		utils.Unauthorized(c, err.Error())
		return
	}
	utils.OK(c, tokens)
}

// Me GET /me
func (h *Handler) Me(c *gin.Context) {
	id := mustAccountID(c)
	acc, err := h.svc.GetAccount(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, acc)
}

// UpdateMe PUT /me
func (h *Handler) UpdateMe(c *gin.Context) {
	id := mustAccountID(c)
	var body struct {
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	acc, err := h.svc.UpdateAccount(c.Request.Context(), id, UpdateAccountInput{
		Email: body.Email, Phone: body.Phone,
	})
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, acc)
}

// DeleteMe DELETE /me
func (h *Handler) DeleteMe(c *gin.Context) {
	id := mustAccountID(c)
	if err := h.svc.SoftDeleteAccount(c.Request.Context(), id); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "account deleted"})
}

// mustAccountID extracts the authenticated account ID from Gin context.
func mustAccountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}
