package candidate

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the candidate Service.
type Handler struct{ svc *Service }

// NewHandler creates a candidate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateProfile POST /me/candidate-profile
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
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, cand)
}

// GetMyProfile GET /me/candidate-profile
func (h *Handler) GetMyProfile(c *gin.Context) {
	cand, err := h.svc.GetMyProfile(c.Request.Context(), utils.MustAccountID(c))
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// PatchMyProfile PATCH /me/candidate-profile
func (h *Handler) PatchMyProfile(c *gin.Context) {
	var req request.PatchProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	cand, err := h.svc.PatchMyProfile(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		if errors.Is(err, ErrActiveRegistration) {
			utils.Conflict(c, err.Error())
			return
		}
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// UploadDocument POST /me/candidate-profile/documents
// GetUploadSignature GET /candidates/upload-signature
func (h *Handler) GetUploadSignature(c *gin.Context) {
	sig, err := h.svc.GetUploadSignature(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Không thể tạo chữ ký tải lên")
		return
	}
	utils.OK(c, sig)
}

// UploadDocument POST /candidates/:id/documents
func (h *Handler) UploadDocument(c *gin.Context) {
	var req request.UploadDocumentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	doc, err := h.svc.UploadDocument(c.Request.Context(), utils.MustAccountID(c), req)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, doc)
}

// AdminListCandidates GET /admin/candidates
func (h *Handler) AdminListCandidates(c *gin.Context) {
	var req request.AdminListCandidatesRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	items, meta, err := h.svc.AdminListCandidates(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, "Không thể tải danh sách hồ sơ")
		return
	}
	utils.OKWithMeta(c, items, &meta)
}

// AdminListDuplicates GET /admin/candidates/duplicates
// NOTE: This route must be registered BEFORE /admin/candidates/:id to avoid path conflict.
func (h *Handler) AdminListDuplicates(c *gin.Context) {
	items, err := h.svc.AdminListDuplicates(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Không thể kiểm tra hồ sơ trùng")
		return
	}
	utils.OK(c, items)
}

// AdminGetCandidate GET /admin/candidates/:id
func (h *Handler) AdminGetCandidate(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID không hợp lệ")
		return
	}
	cand, err := h.svc.AdminGetCandidate(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// AdminApproveDocument POST /admin/candidates/:id/approve
func (h *Handler) AdminApproveDocument(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID không hợp lệ")
		return
	}
	if err := h.svc.AdminApproveDocument(c.Request.Context(), utils.MustAccountID(c), id); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Hồ sơ đã được duyệt"})
}

// AdminRejectDocument POST /admin/candidates/:id/reject
func (h *Handler) AdminRejectDocument(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID không hợp lệ")
		return
	}
	var req request.AdminRejectDocumentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.AdminRejectDocument(c.Request.Context(), utils.MustAccountID(c), id, req); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Hồ sơ đã bị từ chối"})
}

// AdminGetRoster GET /admin/exam-sessions/:id/roster
func (h *Handler) AdminGetRoster(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	items, err := h.svc.AdminGetRoster(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "Không thể tải danh sách điểm danh")
		return
	}
	utils.OK(c, items)
}

