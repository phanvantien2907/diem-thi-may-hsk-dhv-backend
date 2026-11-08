package handlers

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hskvinh/backend/internal/cccd/models"
	"github.com/hskvinh/backend/internal/cccd/worker"
	"github.com/hskvinh/backend/internal/utils"
)

const (
	MaxUploadSizeBytes = 5 * 1024 * 1024 // 5 MB
	DefaultTempDir     = "/tmp/cccd_uploads"
)

// UploadHandler processes asynchronous CCCD image uploads.
type UploadHandler struct {
	workerPool *worker.WorkerPool
	tempDir    string
}

// NewUploadHandler constructs an UploadHandler.
func NewUploadHandler(wp *worker.WorkerPool, tempDir string) *UploadHandler {
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "cccd_uploads")
	}
	_ = os.MkdirAll(tempDir, 0700)
	return &UploadHandler{
		workerPool: wp,
		tempDir:    tempDir,
	}
}

// Upload POST /api/v1/cccd/upload
// @Summary Tải lên ảnh CCCD xử lý bất đồng bộ siêu tốc (< 50ms)
// @Description Tiếp nhận ảnh CCCD mặt trước/mặt sau từ ổ đĩa cục bộ của người dùng. Kiểm tra nhanh định dạng và kích thước, lưu vào phân vùng tạm an toàn, tạo task_id và trả về 202 Accepted ngay lập tức. Kết quả trích xuất dạng chữ (không có sinh trắc học/khuôn mặt) được đẩy qua WebSocket.
// @Tags CCCD OCR
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param image formData file true "Ảnh CCCD/CMND (.jpg, .jpeg, .png)"
// @Success 202 {object} map[string]interface{} "Tiếp nhận tác vụ thành công"
// @Failure 400 {object} map[string]interface{} "File không hợp lệ hoặc quá dung lượng"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực"
// @Failure 503 {object} map[string]interface{} "Hàng đợi xử lý quá tải"
// @Router /cccd/upload [post]
func (h *UploadHandler) Upload(c *gin.Context) {
	// 1. Extract file header
	fileHeader, err := c.FormFile("image")
	if err != nil {
		fileHeader, err = c.FormFile("file")
	}
	if err != nil || fileHeader == nil {
		utils.BadRequest(c, "Thiếu file ảnh trong request (form field 'image' hoặc 'file')")
		return
	}

	// 2. Fast validation: size check
	if fileHeader.Size > MaxUploadSizeBytes {
		utils.BadRequest(c, fmt.Sprintf("Kích thước file vượt quá giới hạn tối đa (%d MB)", MaxUploadSizeBytes/(1024*1024)))
		return
	}

	// 3. Fast validation: extension check
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		utils.BadRequest(c, "Định dạng file không được hỗ trợ (chỉ chấp nhận .jpg, .jpeg, .png)")
		return
	}

	// 4. Generate unique task ID
	taskID := uuid.New().String()
	destPath := filepath.Join(h.tempDir, fmt.Sprintf("%s%s", taskID, ext))

	// 5. Stream file directly into temp directory
	if err := saveUploadedFileDirect(fileHeader, destPath); err != nil {
		utils.InternalServerError(c, "Không thể lưu file ảnh tạm thời")
		return
	}

	// 6. Get Account ID from JWT context and optional card side ("front" | "back")
	userID := utils.MustAccountID(c)
	side := strings.ToLower(strings.TrimSpace(c.PostForm("side")))
	if side == "" {
		side = strings.ToLower(strings.TrimSpace(c.Query("side")))
	}
	if side != "back" {
		side = "front"
	}

	// 7. Enqueue task for Go Worker Pool
	task := models.OCRTask{
		TaskID:    taskID,
		UserID:    userID,
		Side:      side,
		FilePath:  destPath,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.workerPool.Enqueue(task); err != nil {
		// Clean up uploaded file immediately if queue is saturated
		_ = os.Remove(destPath)
		utils.ServiceUnavailable(c, "Hệ thống trích xuất OCR đang quá tải, vui lòng thử lại sau")
		return
	}

	// 8. Respond in < 50ms with 202 Accepted
	utils.Accepted(c, gin.H{
		"task_id": taskID,
		"side":    side,
		"status":  models.StatusQueued,
		"message": "Đã tiếp nhận ảnh CCCD, tác vụ đang được xử lý trong nền",
	})
}

// GetTaskStatus retrieves the current task status from the worker pool for polling fallback.
func (h *UploadHandler) GetTaskStatus(taskID string, userID int64) (models.OCREventPayload, bool) {
	if h.workerPool == nil {
		return models.OCREventPayload{}, false
	}
	return h.workerPool.GetTaskStatus(taskID, userID)
}

// saveUploadedFileDirect streams multipart file into destination with restricted file permissions.
func saveUploadedFileDirect(file *multipart.FileHeader, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

