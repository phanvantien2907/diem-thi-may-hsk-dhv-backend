package exam

import (
	"github.com/gin-gonic/gin"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/dto/response"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the exam Service.
type Handler struct{ svc *Service }

// NewHandler creates an exam Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListExamTypes GET /exam-types
// @Summary Danh sách loại kỳ thi
// @Description Lấy danh sách các loại chứng chỉ/kỳ thi (HSK 1, HSK 2...).
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /exam-types [get]
func (h *Handler) ListExamTypes(c *gin.Context) {
	list, err := h.svc.ListExamTypes(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách loại thi thất bại")
		return
	}
	utils.OK(c, list)
}

// ListExamRooms GET /exam-rooms
func (h *Handler) ListExamRooms(c *gin.Context) {
	list, err := h.svc.ListExamRooms(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách phòng thi thất bại")
		return
	}
	utils.OK(c, list)
}

// ListExamSessions GET /exam-sessions?exam_type_id=&from=&to=
// @Summary Danh sách ca thi
// @Description Xem các ca thi đang mở hoặc sắp diễn ra.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /exam-sessions [get]
func (h *Handler) ListExamSessions(c *gin.Context) {
	var f examreq.ListExamSessionsFilterDTO
	_ = c.ShouldBindQuery(&f) // all params optional
	list, err := h.svc.ListExamSessionsFiltered(c.Request.Context(), f)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách ca thi thất bại")
		return
	}
	utils.OK(c, list)
}

// GetExamSession GET /exam-sessions/:id
func (h *Handler) GetExamSession(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	sess, err := h.svc.GetExamSession(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, sess)
}

// ListExamSeats GET /exam-sessions/:id/seats
// @Summary Sơ đồ ghế ngồi
// @Description Xem danh sách ghế và trạng thái (trống, đang giữ, đã đặt) của một ca thi.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /exam-sessions/:id/seats [get]
func (h *Handler) ListExamSeats(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	seats, err := h.svc.ListExamSeats(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách ghế thi thất bại")
		return
	}
	utils.OK(c, seats)
}

// GenerateSeats POST /admin/exam-sessions/:id/seats/generate
func (h *Handler) GenerateSeats(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	actorID := utils.MustAccountID(c)
	created, err := h.svc.GenerateExamSeats(c.Request.Context(), id, actorID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, response.GenerateSeatsResponseDTO{SessionID: id, CreatedSeats: created})
}

// CancelExamSession POST /admin/exam-sessions/:id/cancel
// @Summary Hủy ca thi (Admin)
// @Description Admin hủy một ca thi (khi chưa có ai đăng ký).
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/:id/cancel [post]
func (h *Handler) CancelExamSession(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	actorID := utils.MustAccountID(c)
	if err := h.svc.CancelExamSession(c.Request.Context(), id, actorID); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Ca thi đã được hủy"})
}

// BatchCreateExamSessions POST /admin/exam-sessions/batch-create
func (h *Handler) BatchCreateExamSessions(c *gin.Context) {
	var req examreq.BatchCreateExamSessionsRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	actorID := utils.MustAccountID(c)
	list, err := h.svc.BatchCreateExamSessions(c.Request.Context(), req.Sessions, actorID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, list)
}
