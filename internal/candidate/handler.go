package candidate

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the candidate Service.
type Handler struct{ svc *Service }

// NewHandler creates a candidate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateProfile POST /me/candidate-profile
// @Summary Tạo hồ sơ thí sinh
// @Description Khởi tạo hồ sơ chi tiết cho thí sinh (sau khi đăng ký tài khoản).
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.CreateProfileRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/candidate-profile [post]
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
// @Summary Lấy hồ sơ thí sinh
// @Description Xem hồ sơ chi tiết của bản thân.
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy hồ sơ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/candidate-profile [get]
func (h *Handler) GetMyProfile(c *gin.Context) {
	cand, err := h.svc.GetMyProfile(c.Request.Context(), utils.MustAccountID(c))
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// GetMyDocuments GET /me/candidate-profile/documents
// @Summary Lấy danh sách giấy tờ tùy thân
// @Description Xem danh sách giấy tờ tùy thân (CCCD/Passport) của bản thân.
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy hồ sơ thí sinh"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/candidate-profile/documents [get]
func (h *Handler) GetMyDocuments(c *gin.Context) {
	docs, err := h.svc.GetMyDocuments(c.Request.Context(), utils.MustAccountID(c))
	if err != nil {
		// Business error (no candidate profile) → 404
		if err.Error() == "chưa có hồ sơ thí sinh" {
			utils.NotFound(c, err.Error())
			return
		}
		// Infrastructure error → 500 with generic Vietnamese message
		utils.InternalServerError(c, "Không thể tải danh sách giấy tờ")
		return
	}
	utils.OK(c, ToDocumentResponseDTOList(docs))
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
// @Summary GetUploadSignature
// @Description Endpoint for GetUploadSignature
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Router /candidates/upload-signature [get]
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
// @Summary Danh sách thí sinh (Admin)
// @Description Lọc, phân trang và xem danh sách toàn bộ thí sinh trên hệ thống.
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.AdminListCandidatesRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/candidates [get]
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
	items, meta, err := h.svc.AdminListCandidatesFiltered(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, "Không thể tải danh sách hồ sơ")
		return
	}
	utils.OKWithMeta(c, items, &meta)
}

// AdminCandidateMetrics GET /admin/candidates/metrics
// @Summary Thống kê hồ sơ thí sinh (Admin)
// @Description Trả về tổng số hồ sơ và số lượng theo trạng thái giấy tờ mới nhất.
// @Tags Candidate
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/candidates/metrics [get]
func (h *Handler) AdminCandidateMetrics(c *gin.Context) {
	metrics, err := h.svc.AdminCandidateMetrics(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Không thể tải thống kê hồ sơ")
		return
	}
	utils.OK(c, metrics)
}

// AdminExportCandidates GET /admin/candidates/export
// @Summary Xuất danh sách thí sinh (Admin)
// @Description Xuất danh sách hồ sơ phù hợp với bộ lọc thành tệp PDF.
// @Tags Candidate
// @Produce application/pdf
// @Security BearerAuth
// @Param status query string false "Trạng thái giấy tờ: all, pending, verified, rejected (mặc định all)"
// @Param level query string false "Cấp độ HSK"
// @Param search query string false "Tìm theo họ tên hoặc CCCD"
// @Param exam_date query string true "Ngày thi của đợt thi, định dạng YYYY-MM-DD"
// @Success 200 {file} binary "Tệp PDF"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/candidates/export [get]
func (h *Handler) AdminExportCandidates(c *gin.Context) {
	var req request.AdminListCandidatesRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if _, err := time.Parse("2006-01-02", req.ExamDate); err != nil {
		utils.BadRequest(c, "Ngày thi không hợp lệ, định dạng đúng là YYYY-MM-DD")
		return
	}
	pdf, err := h.svc.AdminExportCandidates(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, "Không thể xuất danh sách hồ sơ")
		return
	}
	filename := exportFilename(req.Status, req.ExamDate)
	c.DataFromReader(http.StatusOK, int64(len(pdf)), "application/pdf", bytes.NewReader(pdf), map[string]string{
		"Content-Disposition": `attachment; filename="` + filename + `"`,
	})
}

func exportFilename(status, examDate string) string {
	date, _ := time.Parse("2006-01-02", examDate)
	datePart := date.Format("02-01-2006")
	switch status {
	case "rejected":
		return "ds_ho_so_thi_sinh_tu_choi_" + datePart + ".pdf"
	case "pending":
		return "ds_ho_so_thi_sinh_cho_duyet_" + datePart + ".pdf"
	case "verified":
		return "ds_ho_so_thi_sinh_da_duyet_" + datePart + ".pdf"
	default:
		return "ds_thi_sinh_" + datePart + ".pdf"
	}
}

// AdminListDuplicates GET /admin/candidates/duplicates
// NOTE: This route must be registered BEFORE /admin/candidates/:id to avoid path conflict.
// @Summary AdminListDuplicates
// @Description Endpoint for AdminListDuplicates
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Router /admin/candidates/duplicates [get]
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
// @Summary Duyệt giấy tờ
// @Description Admin phê duyệt giấy tờ tùy thân của thí sinh.
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/candidates/:id/approve [post]
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
// @Summary Danh sách phòng thi
// @Description Admin xem danh sách thí sinh trong một phòng thi.
// @Tags Candidate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/:id/roster [get]
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
