package cccd

import (
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/cccd/dto/request"
	"github.com/hskvinh/backend/internal/cccd/handlers"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the CCCD OCR Service.
type Handler struct {
	svc           *Service
	uploadHandler *handlers.UploadHandler
}

// NewHandler creates a CCCD Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// SetUploadHandler configures the fast-path upload handler.
func (h *Handler) SetUploadHandler(uh *handlers.UploadHandler) {
	h.uploadHandler = uh
}

// Upload POST /api/v1/cccd/upload
// Delegates to the fast-path UploadHandler (< 50ms 202 Accepted).
func (h *Handler) Upload(c *gin.Context) {
	if h.uploadHandler != nil {
		h.uploadHandler.Upload(c)
		return
	}
	h.Extract(c)
}

// Extract POST /api/v1/cccd/extract
// @Summary Trích xuất thông tin CCCD/CMND/Thẻ căn cước bằng AI (bất đồng bộ 202 Accepted)
// @Description Nhận file ảnh qua multipart/form-data (trường `image` hoặc `file`). Mặc định trả về 202 Accepted ngay lập tức và đẩy kết quả qua WebSocket event `CCCD_EXTRACTED`; có thể tra cứu lại qua `GET /cccd/jobs/{id}`. Truyền `?sync=true` để chờ kết quả trực tiếp (có thể vượt quá WriteTimeout của server với ảnh khó).
// @Tags CCCD OCR
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param image formData file true "Ảnh mặt trước hoặc mặt sau CCCD/CMND"
// @Param sync query bool false "Chờ kết quả đồng bộ (mặc định false)"
// @Param X-Request-ID header string false "Mã định danh tác vụ do client tự sinh (tùy chọn)"
// @Success 202 {object} response.AsyncJobResponseDTO "Đã tiếp nhận xử lý bất đồng bộ"
// @Success 200 {object} response.IdCardResponseDTO "Kết quả trích xuất đồng bộ (khi sync=true)"
// @Failure 400 {object} map[string]interface{} "Thiếu ảnh hoặc ảnh vượt dung lượng"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 503 {object} map[string]interface{} "Hàng đợi xử lý đang quá tải"
// @Router /cccd/extract [post]
func (h *Handler) Extract(c *gin.Context) {
	var q request.ExtractQueryDTO
	if err := c.ShouldBindQuery(&q); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}

	fileHeader, err := c.FormFile("image")
	if err != nil {
		fileHeader, err = c.FormFile("file")
	}
	if err != nil || fileHeader == nil {
		utils.BadRequest(c, "Thiếu file ảnh trong request")
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		utils.BadRequest(c, "Không thể mở file ảnh tải lên")
		return
	}
	defer f.Close()

	maxBytes := h.svc.Config().MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	rawBytes, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		utils.BadRequest(c, "Không thể đọc dữ liệu file ảnh")
		return
	}

	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))

	if q.Sync {
		utils.OK(c, h.svc.Process(c.Request.Context(), rawBytes, requestID))
		return
	}

	job, err := h.svc.EnqueueAsync(utils.MustAccountID(c), rawBytes, requestID)
	if err != nil {
		var pErr *PipelineError
		if errors.As(err, &pErr) && pErr.Code != ErrCodeExtractionFailed {
			utils.BadRequest(c, pErr.Message)
			return
		}
		utils.ServiceUnavailable(c, "Hệ thống đọc giấy tờ đang quá tải, vui lòng thử lại sau")
		return
	}

	dto := ToAsyncJobResponseDTO(job)
	dto.Message = "Đã tiếp nhận ảnh. Kết quả sẽ được gửi qua WebSocket (CCCD_EXTRACTED)."
	utils.Accepted(c, dto)
}

// GetJobStatus GET /api/v1/cccd/jobs/:id
// @Summary Tra cứu trạng thái tác vụ đọc CCCD theo request_id
// @Description Chỉ chủ sở hữu tác vụ mới tra cứu được. Tác vụ tự hết hạn sau 10 phút.
// @Tags CCCD OCR
// @Produce json
// @Security BearerAuth
// @Param id path string true "Request ID của tác vụ"
// @Success 200 {object} response.AsyncJobResponseDTO "Trạng thái và kết quả trích xuất"
// @Failure 404 {object} map[string]interface{} "Không tìm thấy hoặc đã hết hạn"
// @Router /cccd/jobs/{id} [get]
func (h *Handler) GetJobStatus(c *gin.Context) {
	reqID := strings.TrimSpace(c.Param("id"))
	if reqID == "" {
		utils.BadRequest(c, "Thiếu mã tác vụ (request_id)")
		return
	}

	userID := utils.MustAccountID(c)
	if h.uploadHandler != nil {
		if taskPayload, ok := h.uploadHandler.GetTaskStatus(reqID, userID); ok {
			utils.OK(c, taskPayload)
			return
		}
	}

	job, ok := h.svc.GetJob(reqID, userID)
	if !ok {
		utils.NotFound(c, "Không tìm thấy tác vụ đọc giấy tờ hoặc tác vụ đã hết hạn")
		return
	}
	utils.OK(c, ToAsyncJobResponseDTO(job))
}

