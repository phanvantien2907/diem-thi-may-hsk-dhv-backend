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

// ListAdminExamSessions GET /admin/exam-sessions?exam_type_id=&from=&to=
// @Summary Danh sách toàn bộ ca thi (Admin)
// @Description Admin xem các ca thi published, draft, scheduled và cancelled.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions [get]
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

// UpdatePublicationStatus PATCH /admin/exam-sessions/:id/publication-status
// @Summary Cập nhật trạng thái phát hành ca thi (Admin)
// @Description Chuyển ca thi giữa draft, published và scheduled.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID ca thi"
// @Param request body examreq.UpdatePublicationStatusRequestDTO true "Trạng thái phát hành mới"
// @Success 200 {object} response.UpdatePublicationStatusResponseDTO
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/:id/publication-status [patch]
func (h *Handler) UpdatePublicationStatus(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req examreq.UpdatePublicationStatusRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	result, err := h.svc.UpdatePublicationStatus(c.Request.Context(), id, utils.MustAccountID(c), req)
	if err != nil {
		if errors.Is(err, errExamSessionNotFound) {
			utils.NotFound(c, err.Error())
		} else if errors.Is(err, ErrSessionScheduleIncomplete) || isPublicationValidationError(err) {
			utils.BadRequest(c, err.Error())
		} else {
			utils.InternalServerError(c, "Cập nhật trạng thái phát hành thất bại")
		}
		return
	}
	utils.OK(c, result)
}

func isPublicationValidationError(err error) bool {
	message := err.Error()
	return message == "Trạng thái phát hành không hợp lệ" ||
		message == "Ca thi hẹn giờ phải có exam_session_publish_at trong tương lai" ||
		message == "Ca thi lưu nháp không được có exam_session_publish_at" ||
		message == "Ca thi công khai không được có exam_session_publish_at trong tương lai" ||
		message == "Không thể cập nhật trạng thái phát hành của ca thi đã hủy" ||
		message == "exam: parse publication time: exam_session_publish_at phải có định dạng RFC3339 hợp lệ"
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

// ClearCache godoc
// @Summary      Xoá bộ nhớ đệm Ca thi
// @Description  Xoá cache danh sách loại thi và ca thi trên Redis để phục vụ FE/Admin làm mới dữ liệu
// @Tags         Exam Sessions
// @Produce      json
// @Success      200  {object}  map[string]interface{} "Thành công"
// @Router       /exam-sessions/clear-cache [post]
func (h *Handler) ClearCache(c *gin.Context) {
	if err := h.svc.ClearCache(c.Request.Context()); err != nil {
		utils.InternalServerError(c, "Xóa bộ nhớ đệm ca thi thất bại")
		return
	}
	utils.OK(c, gin.H{"message": "Đã làm mới bộ nhớ đệm ca thi"})
}

// AdminListExamPeriods GET /admin/exam-periods
// @Summary Danh sách đợt thi (Admin)
// @Description Lấy danh sách các đợt thi (exam periods) với bộ lọc trạng thái và phân trang.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param status query string false "Trạng thái (draft, published, scheduled, closed, cancelled)"
// @Param limit query int false "Số lượng bản ghi mỗi trang (mặc định 20, tối đa 100)"
// @Param offset query int false "Vị trí bắt đầu"
// @Success 200 {array} response.ExamPeriodResponseDTO "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods [get]
func (h *Handler) AdminListExamPeriods(c *gin.Context) {
	var f examreq.ListExamPeriodsFilterDTO
	_ = c.ShouldBindQuery(&f)
	list, err := h.svc.AdminListExamPeriods(c.Request.Context(), f)
	if err != nil {
		utils.InternalServerError(c, "Lấy danh sách kỳ thi thất bại")
		return
	}
	utils.OK(c, list)
}

// AdminGetExamPeriod GET /admin/exam-periods/:id
// @Summary Chi tiết đợt thi (Admin)
// @Description Lấy thông tin chi tiết một đợt thi kèm danh sách các ca thi trực thuộc.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID đợt thi"
// @Success 200 {object} response.ExamPeriodResponseDTO "Thành công"
// @Failure 400 {object} map[string]interface{} "ID không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods/{id} [get]
func (h *Handler) AdminGetExamPeriod(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID kỳ thi không hợp lệ")
		return
	}
	period, err := h.svc.AdminGetExamPeriod(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrExamPeriodNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, "Lấy chi tiết kỳ thi thất bại")
		return
	}
	utils.OK(c, period)
}

// AdminCreateExamPeriod POST /admin/exam-periods
// @Summary Tạo đợt thi mới (Admin)
// @Description Tạo một đợt thi mới (ở trạng thái draft).
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body examreq.CreateExamPeriodRequestDTO true "Dữ liệu đợt thi"
// @Success 201 {object} response.ExamPeriodResponseDTO "Tạo thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods [post]
func (h *Handler) AdminCreateExamPeriod(c *gin.Context) {
	var req examreq.CreateExamPeriodRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminCreateExamPeriod(c.Request.Context(), req, actorID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, res)
}

// AdminDuplicateExamPeriod POST /admin/exam-periods/:id/duplicate
// @Summary Nhân bản đợt thi (Admin)
// @Description Tạo đợt thi mới từ một đợt thi cũ, sao chép cấu hình các ca thi nhưng reset trạng thái sang draft và đặt ngày thi/hạn đăng ký về NULL. Không sao chép thí sinh hay đăng ký.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID đợt thi nguồn"
// @Param request body examreq.DuplicateExamPeriodRequestDTO false "Tên và mã tùy chọn cho đợt thi mới"
// @Success 201 {object} response.DuplicateExamPeriodResponseDTO "Nhân bản thành công"
// @Failure 400 {object} map[string]interface{} "ID hoặc dữ liệu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi nguồn"
// @Failure 409 {object} map[string]interface{} "Mã đợt thi đã tồn tại"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods/{id}/duplicate [post]
func (h *Handler) AdminDuplicateExamPeriod(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID kỳ thi không hợp lệ")
		return
	}
	var req examreq.DuplicateExamPeriodRequestDTO
	_ = c.ShouldBindJSON(&req)
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminDuplicateExamPeriod(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamPeriodNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, ErrExamPeriodCodeExists) {
			utils.Conflict(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, res)
}

// AdminUpdateExamPeriod PUT /admin/exam-periods/:id
// @Summary Cập nhật đợt thi (Admin)
// @Description Cập nhật thông tin đợt thi. Khi ở draft, cập nhật exam_date/registration_deadline sẽ tự động áp dụng cho các ca thi con chưa có lịch.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID đợt thi"
// @Param request body examreq.UpdateExamPeriodRequestDTO true "Dữ liệu cập nhật"
// @Success 200 {object} response.ExamPeriodResponseDTO "Cập nhật thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Failure 409 {object} map[string]interface{} "Không thể sửa ngày khi đợt thi không ở trạng thái draft"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods/{id} [put]
func (h *Handler) AdminUpdateExamPeriod(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID kỳ thi không hợp lệ")
		return
	}
	var req examreq.UpdateExamPeriodRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminUpdateExamPeriod(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamPeriodNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, ErrNonDraftDateModification) {
			utils.Conflict(c, err.Error())
			return
		}
		if IsValidationError(err) {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminRescheduleExamPeriod POST /admin/exam-periods/:id/reschedule
// @Summary Đổi lịch đợt thi (Admin)
// @Description Đổi lịch ngày thi và hạn chót cho toàn bộ đợt thi và các ca thi con thuộc đợt.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID đợt thi"
// @Param request body examreq.RescheduleExamPeriodRequestDTO true "Thông tin đổi lịch"
// @Success 200 {object} response.RescheduleExamPeriodResponseDTO "Đổi lịch thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods/{id}/reschedule [post]
func (h *Handler) AdminRescheduleExamPeriod(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID kỳ thi không hợp lệ")
		return
	}
	var req examreq.RescheduleExamPeriodRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminRescheduleExamPeriod(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamPeriodNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminClosePeriodRegistration POST /admin/exam-periods/:id/close-registration
// @Summary Đóng đăng ký đợt thi sớm (Admin)
// @Description Chủ động đóng cổng đăng ký cho toàn bộ các ca thi thuộc đợt thi trước hạn chót.
// @Tags Exam Periods
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID đợt thi"
// @Param request body examreq.CloseRegistrationRequestDTO false "Lý do đóng đăng ký"
// @Success 200 {object} response.ExamPeriodResponseDTO "Đóng đăng ký thành công"
// @Failure 400 {object} map[string]interface{} "ID không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy đợt thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-periods/{id}/close-registration [post]
func (h *Handler) AdminClosePeriodRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID kỳ thi không hợp lệ")
		return
	}
	var req examreq.CloseRegistrationRequestDTO
	_ = c.ShouldBindJSON(&req)
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminCloseRegistration(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamPeriodNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminCloseSessionRegistration POST /admin/exam-sessions/:id/close-registration
// @Summary Đóng đăng ký ca thi sớm (Admin)
// @Description Chủ động đóng cổng đăng ký cho một ca thi đơn lẻ trước hạn chót.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID ca thi"
// @Param request body examreq.CloseRegistrationRequestDTO false "Lý do đóng đăng ký"
// @Success 200 {object} response.ExamSessionResponseDTO "Đóng đăng ký ca thi thành công"
// @Failure 400 {object} map[string]interface{} "ID không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/{id}/close-registration [post]
func (h *Handler) AdminCloseSessionRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req examreq.CloseRegistrationRequestDTO
	_ = c.ShouldBindJSON(&req)
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminCloseSessionRegistration(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamSessionNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminUpdateExamSession PUT /admin/exam-sessions/:id
// @Summary Cập nhật ca thi (Admin)
// @Description Chỉnh sửa thông tin lịch thi, ca thi, lệ phí, số lượng chỗ ngồi hoặc hạn chót đăng ký cho một ca thi. Chỉ cho phép sửa lịch/lệ phí khi ca thi ở draft hoặc chưa có thí sinh đăng ký.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID ca thi"
// @Param request body examreq.UpdateExamSessionRequestDTO true "Dữ liệu ca thi cập nhật"
// @Success 200 {object} response.ExamSessionResponseDTO "Cập nhật thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy ca thi"
// @Failure 409 {object} map[string]interface{} "Không thể chỉnh sửa ca thi đã phát hành hoặc đã có thí sinh đăng ký"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/{id} [put]
func (h *Handler) AdminUpdateExamSession(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req examreq.UpdateExamSessionRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminUpdateExamSession(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamSessionNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, ErrSessionNotEditable) {
			utils.Conflict(c, err.Error())
			return
		}
		if errors.Is(err, ErrExamSessionCancelled) || IsValidationError(err) {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OK(c, res)
}

// AdminDuplicateExamSession POST /admin/exam-sessions/:id/duplicate
// @Summary Nhân bản ca thi (Admin)
// @Description Nhân bản một ca thi thuộc cùng đợt thi. Sao chép loại thi, mô tả, phòng thi, lệ phí và phụ phí; đặt ngày thi, số lượng chỗ và hạn chót về NULL; trạng thái về draft; tên thêm hậu tố (Copy); ghi nhận lineage trong duplicated_from_id. Không sao chép thí sinh, đơn đăng ký hay ghế ngồi.
// @Tags Exam
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID ca thi nguồn"
// @Param request body examreq.DuplicateExamSessionRequestDTO false "Tùy chọn tên cho ca thi mới"
// @Success 201 {object} response.ExamSessionResponseDTO "Nhân bản ca thi thành công"
// @Failure 400 {object} map[string]interface{} "ID không hợp lệ hoặc dữ liệu sai"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy ca thi nguồn"
// @Failure 409 {object} map[string]interface{} "Đợt thi cha đã đóng hoặc đã hủy"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/exam-sessions/{id}/duplicate [post]
func (h *Handler) AdminDuplicateExamSession(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID ca thi không hợp lệ")
		return
	}
	var req examreq.DuplicateExamSessionRequestDTO
	_ = c.ShouldBindJSON(&req)
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	actorID := utils.MustAccountID(c)
	res, err := h.svc.AdminDuplicateExamSession(c.Request.Context(), id, actorID, req)
	if err != nil {
		if errors.Is(err, ErrExamSessionNotFound) {
			utils.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, ErrParentPeriodClosed) {
			utils.Conflict(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, res)
}

