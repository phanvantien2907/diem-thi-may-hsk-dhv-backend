package candidate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the candidate Service.
type Handler struct{ svc *Service }

// NewHandler creates a candidate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateProfile POST /candidates
func (h *Handler) CreateProfile(c *gin.Context) {
	var req request.CreateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	cand, err := h.svc.CreateProfile(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, cand)
}

// UpdateProfile PUT /candidates/:id
func (h *Handler) UpdateProfile(c *gin.Context) {
	cid, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ứng viên không hợp lệ")
		return
	}
	var req request.UpdateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	cand, err := h.svc.UpdateProfile(c.Request.Context(), cid, req)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// UploadDocument POST /candidates/:id/documents
func (h *Handler) UploadDocument(c *gin.Context) {
	cid, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ứng viên không hợp lệ")
		return
	}
	var req request.UploadDocumentRequestDTO
	if err := c.ShouldBind(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	doc, err := h.svc.UploadDocument(c.Request.Context(), cid, req)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, doc)
}
