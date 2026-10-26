package exam

import (
	"errors"

	"github.com/gin-gonic/gin"
	examreq "github.com/hskvinh/backend/internal/exam/dto/request"
	"github.com/hskvinh/backend/internal/exam/dto/response"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the exam Service.
type Handler struct{ svc *Service }

// NewHandler creates an exam Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// bindJSON binds and validates the request body, writing a 400 on failure.
func bindJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return false
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return false
	}
	return true
}

// optionalJSON binds the request body but tolerates an empty body (all fields optional).
func optionalJSON(c *gin.Context, req any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	return bindJSON(c, req)
}

// sessionIDParam parses and validates the :id path parameter.
func sessionIDParam(c *gin.Context, label string) (int64, bool) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, label+" không hợp lệ")
		return 0, false
	}
	return id, true
}

// failState maps a service error to the correct HTTP response.
// A guard rejection is reported as 409 with the INVALID_STATE_TRANSITION code so the
// frontend can react to an illegal state change distinctly from a validation error.
func failState(c *gin.Context, err error, fallback string) {
	switch {
	case IsInvalidStateTransition(err):
		utils.ConflictWithCode(c, "INVALID_STATE_TRANSITION", err.Error())
	case IsValidationError(err):
		utils.BadRequest(c, err.Error())
	case errors.Is(err, ErrExamSessionNotFound), errors.Is(err, ErrExamPeriodNotFound):
		utils.NotFound(c, err.Error())
	case errors.Is(err, ErrSessionScheduleIncomplete),
		errors.Is(err, ErrSessionNotEditable),
		errors.Is(err, ErrNonDraftDateModification),
		errors.Is(err, ErrParentPeriodClosed),
		errors.Is(err, ErrExamPeriodCodeExists):
		utils.BadRequest(c, err.Error())
	default:
		utils.InternalServerError(c, fallback)
	}
}

// ListExamTypes GET /exam-types
// @Summary  Danh sách loại kỳ thi
// @Description Lấy danh sách các loại chứng chỉ/kỳ thi (HSK 1, HSK 2...).
// @Tags     Exam
// @Produce  json
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router   /exam-types [get]
func (h *Handler) ListExamTypes(c *gin.Context) {
	list, err := h.svc.ListExamTypes(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách loại thi thất bại")
		return
	}
	utils.OK(c, list)
}

// ListExamSessions GET /exam-sessions
// @Summary  Danh sách ca thi công khai
// @Description Chỉ trả về ca thi đã công khai (kể cả ca hẹn giờ đã tới thời điểm công khai) và thuộc đợt thi không nháp/không hủy. Trường status là trạng thái hiệu lực tính theo thời gian thực.
// @Tags     Exam
// @Produce  json
// @Param    exam_type_id query int    false "ID loại thi"
// @Param    from         query string false "Từ ngày (YYYY-MM-DD)"
// @Param    to           query string false "Đến ngày (YYYY-MM-DD)"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router   /exam-sessions [get]
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

// ListAdminExamSessions GET /admin/exam-sessions
// @Summary  Danh sách toàn bộ ca thi (Admin)
// @Description Trả về mọi ca thi chưa bị xóa, bao gồm cả nháp, hẹn giờ, đã hủy.
// @Tags     Exam
// @Produce  json
// @Security BearerAuth
// @Param    exam_type_id query int    false "ID loại thi"
// @Param    from         query string false "Từ ngày (YYYY-MM-DD)"
// @Param    to           query string false "Đến ngày (YYYY-MM-DD)"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  401 {object} map[string]interface{} "Chưa xác thực"
// @Failure  403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure  500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router   /admin/exam-sessions [get]
func (h *Handler) ListAdminExamSessions(c *gin.Context) {
	var f examreq.ListExamSessionsFilterDTO
	_ = c.ShouldBindQuery(&f)
	list, err := h.svc.ListAdminExamSessionsFiltered(c.Request.Context(), f)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách ca thi quản trị thất bại")
		return
	}
	utils.OK(c, list)
}

// GetExamSession GET /exam-sessions/:id
// @Summary  Chi tiết ca thi
// @Description Trả về ca thi đã công khai kèm trạng thái hiệu lực và các hành động được phép.
// @Tags     Exam
// @Produce  json
// @Param    id path int true "ID ca thi"
// @Success  200 {object} response.ExamSessionResponseDTO
// @Failure  404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Router   /exam-sessions/{id} [get]
func (h *Handler) GetExamSession(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	sess, err := h.svc.GetExamSession(c.Request.Context(), id)
	if err != nil {
		failState(c, err, "Lấy chi tiết ca thi thất bại")
		return
	}
	utils.OK(c, sess)
}

// ListExamSeats GET /exam-sessions/:id/seats
// @Summary  Sơ đồ ghế ngồi
// @Description Xem danh sách ghế và trạng thái (trống, đang giữ, đã đặt) của một ca thi đã công khai.
// @Tags     Exam
// @Produce  json
// @Param    id path int true "ID ca thi"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router   /exam-sessions/{id}/seats [get]
func (h *Handler) ListExamSeats(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
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
// @Summary  Sinh ghế cho ca thi (Admin)
// @Description Sinh đủ số ghế theo sức chứa của ca thi; ghế đã tồn tại được giữ nguyên.
// @Tags     Exam
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Success  201 {object} response.GenerateSeatsResponseDTO
// @Failure  404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Router   /admin/exam-sessions/{id}/seats/generate [post]
func (h *Handler) GenerateSeats(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	created, err := h.svc.GenerateExamSeats(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Sinh ghế thi thất bại")
		return
	}
	utils.Created(c, response.GenerateSeatsResponseDTO{SessionID: id, CreatedSeats: created})
}

// CancelExamSession POST /admin/exam-sessions/:id/cancel
// @Summary  Hủy ca thi (Admin)
// @Description Chỉ hủy được ca thi đang mở hoặc đã đóng. Ca nháp hoặc hẹn giờ phải dùng xóa bản nháp.
// @Tags     Exam
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/cancel [post]
func (h *Handler) CancelExamSession(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	if err := h.svc.CancelExamSession(c.Request.Context(), id, utils.MustAccountID(c)); err != nil {
		failState(c, err, "Hủy ca thi thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Ca thi đã được hủy"})
}

// PublishExamSession POST /admin/exam-sessions/:id/publish
// @Summary  Công khai ngay ca thi (Admin)
// @Description Công khai ca thi nháp hoặc hẹn giờ ngay lập tức. Ca thi phải có ngày thi, sức chứa và hạn đăng ký.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.PublishSessionRequestDTO false "Thời điểm công khai (bỏ trống để công khai ngay)"
// @Success  200 {object} response.UpdateSessionStatusResponseDTO
// @Failure  400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/publish [post]
func (h *Handler) PublishExamSession(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.PublishSessionRequestDTO
	if !optionalJSON(c, &req) {
		return
	}
	res, err := h.svc.PublishExamSession(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Công khai ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// ScheduleSessionPublish POST /admin/exam-sessions/:id/schedule-publish
// @Summary  Lên lịch công khai ca thi (Admin)
// @Description Đặt thời điểm công khai trong tương lai cho ca thi nháp hoặc hẹn giờ.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.PublishSessionRequestDTO true "Thời điểm công khai (RFC3339)"
// @Success  200 {object} response.UpdateSessionStatusResponseDTO
// @Failure  400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/schedule-publish [post]
func (h *Handler) ScheduleSessionPublish(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.PublishSessionRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	if req.PublishAt == "" {
		utils.BadRequest(c, "Thời điểm công khai là bắt buộc")
		return
	}
	res, err := h.svc.PublishExamSession(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Lên lịch công khai ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// UnpublishExamSession POST /admin/exam-sessions/:id/unpublish
// @Summary  Thu hồi công khai ca thi (Admin)
// @Description Ca hẹn giờ sẽ bị hủy hẹn. Ca đang mở chỉ thu hồi được khi chưa có thí sinh đăng ký.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Success  200 {object} response.UpdateSessionStatusResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/unpublish [post]
func (h *Handler) UnpublishExamSession(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	res, err := h.svc.UnpublishExamSession(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Thu hồi công khai ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminCloseSessionRegistration POST /admin/exam-sessions/:id/close-registration
// @Summary  Đóng đăng ký ca thi sớm (Admin)
// @Description Lý do đóng quyết định điều kiện mở lại: manual, full, deadline, period_closed.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.CloseRegistrationRequestDTO false "Lý do đóng đăng ký"
// @Success  200 {object} response.ExamSessionResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/close-registration [post]
func (h *Handler) AdminCloseSessionRegistration(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.CloseRegistrationRequestDTO
	if !optionalJSON(c, &req) {
		return
	}
	res, err := h.svc.CloseSessionRegistration(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Đóng đăng ký ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminReopenSessionRegistration POST /admin/exam-sessions/:id/reopen-registration
// @Summary  Mở lại đăng ký ca thi (Admin)
// @Description Đợt cha phải đang mở đăng ký. Ca đóng do full phải nới sĩ số trước; ca đóng do deadline phải đặt hạn mới; ca đóng do period_closed phải mở lại từ đợt.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.ReopenSessionRegistrationRequestDTO false "Hạn đăng ký mới (bắt buộc khi đóng do deadline)"
// @Success  200 {object} response.ExamSessionResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/reopen-registration [post]
func (h *Handler) AdminReopenSessionRegistration(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.ReopenSessionRegistrationRequestDTO
	if !optionalJSON(c, &req) {
		return
	}
	res, err := h.svc.ReopenSessionRegistration(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Mở lại đăng ký ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// UpdateSessionCapacity POST /admin/exam-sessions/:id/capacity
// @Summary  Đổi sĩ số ca thi (Admin)
// @Description Không được giảm xuống dưới số thí sinh đã đăng ký. Nới sĩ số ca thi đã đầy sẽ tự mở lại đăng ký.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.UpdateCapacityRequestDTO true "Sĩ số mới"
// @Success  200 {object} response.ExamSessionResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/capacity [post]
func (h *Handler) UpdateSessionCapacity(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.UpdateCapacityRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.UpdateSessionCapacity(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Cập nhật sĩ số ca thi thất bại")
		return
	}
	utils.OK(c, res)
}

// DeleteExamSessionDraft DELETE /admin/exam-sessions/:id
// @Summary  Xóa bản nháp ca thi (Admin)
// @Description Chỉ xóa được ca thi nháp hoặc đang hẹn giờ.
// @Tags     Exam Session Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id}/draft [delete]
func (h *Handler) DeleteExamSessionDraft(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	if err := h.svc.DeleteExamSessionDraft(c.Request.Context(), id, utils.MustAccountID(c)); err != nil {
		failState(c, err, "Xóa ca thi nháp thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa ca thi nháp"})
}

// BatchCreateExamSessions POST /admin/exam-sessions/batch-create
// @Summary  Tạo nhanh nhiều ca thi (Admin)
// @Description Tạo nhiều ca thi trong một transaction. Trạng thái cho phép: open, draft, scheduled.
// @Tags     Exam
// @Produce  json
// @Security BearerAuth
// @Param    request body examreq.BatchCreateExamSessionsRequestDTO true "Danh sách ca thi"
// @Success  201 {object} map[string]interface{} "Thành công"
// @Failure  400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Router   /admin/exam-sessions/batch-create [post]
func (h *Handler) BatchCreateExamSessions(c *gin.Context) {
	var req examreq.BatchCreateExamSessionsRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	list, err := h.svc.BatchCreateExamSessions(c.Request.Context(), req.Sessions, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Tạo danh sách ca thi thất bại")
		return
	}
	utils.Created(c, list)
}

// ClearCache POST /exam-sessions/clear-cache
// @Summary  Xóa bộ nhớ đệm ca thi
// @Description Xóa cache danh sách loại thi và ca thi trên Redis để làm mới dữ liệu.
// @Tags     Exam
// @Produce  json
// @Success  200 {object} map[string]interface{} "Thành công"
// @Router   /exam-sessions/clear-cache [post]
func (h *Handler) ClearCache(c *gin.Context) {
	if err := h.svc.ClearCache(c.Request.Context()); err != nil {
		utils.InternalServerError(c, "Xóa bộ nhớ đệm ca thi thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã làm mới bộ nhớ đệm ca thi"})
}

// AdminListExamPeriods GET /admin/exam-periods
// @Summary  Danh sách đợt thi (Admin)
// @Description Lọc theo trạng thái hiệu lực, khoảng ngày bắt đầu/kết thúc.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    status query string false "Trạng thái hiệu lực (draft, published, registration_open, registration_closed, completed, cancelled)"
// @Param    from   query string false "Từ ngày (YYYY-MM-DD)"
// @Param    to     query string false "Đến ngày (YYYY-MM-DD)"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router   /admin/exam-periods [get]
func (h *Handler) AdminListExamPeriods(c *gin.Context) {
	var f examreq.ListExamPeriodsFilterDTO
	if err := c.ShouldBindQuery(&f); err != nil {
		utils.BadRequest(c, "Tham số lọc không hợp lệ")
		return
	}
	list, err := h.svc.AdminListExamPeriods(c.Request.Context(), f)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách đợt thi thất bại")
		return
	}
	utils.OK(c, list)
}

// AdminGetExamPeriod GET /admin/exam-periods/:id
// @Summary  Chi tiết đợt thi (Admin)
// @Description Trả về đợt thi kèm danh sách ca thi con và trạng thái hiệu lực.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Router   /admin/exam-periods/{id} [get]
func (h *Handler) AdminGetExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	res, err := h.svc.AdminGetExamPeriod(c.Request.Context(), id)
	if err != nil {
		failState(c, err, "Lấy chi tiết đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminCreateExamPeriod POST /admin/exam-periods
// @Summary  Tạo đợt thi (Admin)
// @Description Đợt thi mới luôn được tạo ở trạng thái nháp.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    request body examreq.CreateExamPeriodRequestDTO true "Thông tin đợt thi"
// @Success  201 {object} response.ExamPeriodResponseDTO
// @Failure  400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Router   /admin/exam-periods [post]
func (h *Handler) AdminCreateExamPeriod(c *gin.Context) {
	var req examreq.CreateExamPeriodRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminCreateExamPeriod(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Tạo đợt thi thất bại")
		return
	}
	utils.Created(c, res)
}

// AdminDuplicateExamPeriod POST /admin/exam-periods/:id/duplicate
// @Summary  Nhân bản đợt thi (Admin)
// @Description Sao chép đợt thi và toàn bộ ca thi con sang trạng thái nháp trong một transaction.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.DuplicateExamPeriodRequestDTO false "Tên và mã mới"
// @Success  201 {object} response.DuplicateExamPeriodResponseDTO
// @Failure  404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Router   /admin/exam-periods/{id}/duplicate [post]
func (h *Handler) AdminDuplicateExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.DuplicateExamPeriodRequestDTO
	if !optionalJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminDuplicateExamPeriod(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Nhân bản đợt thi thất bại")
		return
	}
	utils.Created(c, res)
}

// AdminUpdateExamPeriod PUT /admin/exam-periods/:id
// @Summary  Chỉnh sửa đợt thi (Admin)
// @Description Nháp và đã công bố cho sửa toàn bộ; đang mở/đã đóng đăng ký chỉ sửa mô tả và hướng dẫn.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.UpdateExamPeriodRequestDTO true "Thông tin cần cập nhật"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id} [put]
func (h *Handler) AdminUpdateExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.UpdateExamPeriodRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminUpdateExamPeriod(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Cập nhật đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminPublishExamPeriod POST /admin/exam-periods/:id/publish
// @Summary  Công bố đợt thi (Admin)
// @Description Cần ít nhất một ca thi hợp lệ. Nháp -> đã công bố.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/publish [post]
func (h *Handler) AdminPublishExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	res, err := h.svc.AdminPublishExamPeriod(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Công bố đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminUnpublishExamPeriod POST /admin/exam-periods/:id/unpublish
// @Summary  Thu hồi công bố đợt thi (Admin)
// @Description Đã công bố thì luôn thu hồi được; đang mở đăng ký chỉ thu hồi được khi chưa có đăng ký.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/unpublish [post]
func (h *Handler) AdminUnpublishExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	res, err := h.svc.AdminUnpublishExamPeriod(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Thu hồi công bố đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminClosePeriodRegistration POST /admin/exam-periods/:id/close-registration
// @Summary  Đóng đăng ký đợt thi sớm (Admin)
// @Description Ca con đang mở chuyển thành closed với lý do period_closed; ca hẹn giờ bị hủy hẹn.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.CloseRegistrationRequestDTO false "Lý do đóng đăng ký"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/close-registration [post]
func (h *Handler) AdminClosePeriodRegistration(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.CloseRegistrationRequestDTO
	if !optionalJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminCloseRegistration(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Đóng đăng ký đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminReopenPeriodRegistration POST /admin/exam-periods/:id/reopen-registration
// @Summary  Mở lại đăng ký đợt thi (Admin)
// @Description Phải đặt hạn đăng ký mới trong tương lai. Chỉ ca đóng do period_closed quay về open.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.ReopenPeriodRegistrationRequestDTO true "Hạn đăng ký mới"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/reopen-registration [post]
func (h *Handler) AdminReopenPeriodRegistration(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.ReopenPeriodRegistrationRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminReopenPeriodRegistration(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Mở lại đăng ký đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminCompleteExamPeriod POST /admin/exam-periods/:id/complete
// @Summary  Đánh dấu hoàn tất đợt thi (Admin)
// @Description Chỉ đợt đã đóng đăng ký mới được đánh dấu hoàn tất; ca con đang mở/đã đóng cũng chuyển sang completed.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/complete [post]
func (h *Handler) AdminCompleteExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	res, err := h.svc.AdminCompleteExamPeriod(c.Request.Context(), id, utils.MustAccountID(c))
	if err != nil {
		failState(c, err, "Hoàn tất đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminCancelExamPeriod POST /admin/exam-periods/:id/cancel
// @Summary  Hủy đợt thi (Admin)
// @Description Mọi ca con chuyển sang cancelled. Mỗi đăng ký phải chọn hoàn tiền hoặc chuyển ca.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.CancelPeriodRequestDTO true "Lý do hủy và hình thức xử lý đăng ký"
// @Success  200 {object} response.ExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/cancel [post]
func (h *Handler) AdminCancelExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.CancelPeriodRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminCancelExamPeriod(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Hủy đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminDeleteExamPeriod DELETE /admin/exam-periods/:id
// @Summary  Xóa bản nháp đợt thi (Admin)
// @Description Chỉ xóa được đợt thi ở trạng thái nháp; ca nháp và ca hẹn giờ bị xóa theo.
// @Tags     Exam Period Status
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Success  200 {object} map[string]interface{} "Thành công"
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id} [delete]
func (h *Handler) AdminDeleteExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	if err := h.svc.AdminDeleteExamPeriod(c.Request.Context(), id, utils.MustAccountID(c)); err != nil {
		failState(c, err, "Xóa đợt thi nháp thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã xóa đợt thi nháp"})
}

// AdminRescheduleExamPeriod POST /admin/exam-periods/:id/reschedule
// @Summary  Dời lịch đợt thi (Admin)
// @Description Không đổi trạng thái. Ghi nhận ngày gốc, tăng số lần dời lịch, dời ngày các ca con và gửi thông báo.
// @Tags     Exam Periods
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID đợt thi"
// @Param    request body examreq.RescheduleExamPeriodRequestDTO true "Thông tin dời lịch"
// @Success  200 {object} response.RescheduleExamPeriodResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-periods/{id}/reschedule [post]
func (h *Handler) AdminRescheduleExamPeriod(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID đợt thi")
	if !ok {
		return
	}
	var req examreq.RescheduleExamPeriodRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminRescheduleExamPeriod(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Dời lịch đợt thi thất bại")
		return
	}
	utils.OK(c, res)
}

// AdminUpdateExamSession PUT /admin/exam-sessions/:id
// @Summary  Chỉnh sửa ca thi (Admin)
// @Description Ca thi đã có đăng ký thì khóa ngày, giờ, lệ phí và hạn đăng ký; ca đã đóng chỉ sửa được ghi chú.
// @Tags     Exam
// @Produce  json
// @Security BearerAuth
// @Param    id path int true "ID ca thi"
// @Param    request body examreq.UpdateExamSessionRequestDTO true "Thông tin cần cập nhật"
// @Success  200 {object} response.ExamSessionResponseDTO
// @Failure  409 {object} map[string]interface{} "Chuyển trạng thái không hợp lệ"
// @Router   /admin/exam-sessions/{id} [put]
func (h *Handler) AdminUpdateExamSession(c *gin.Context) {
	id, ok := sessionIDParam(c, "ID ca thi")
	if !ok {
		return
	}
	var req examreq.UpdateExamSessionRequestDTO
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.svc.AdminUpdateExamSession(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		failState(c, err, "Cập nhật ca thi thất bại")
		return
	}
	utils.OK(c, res)
}
