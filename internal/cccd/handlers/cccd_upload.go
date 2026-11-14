package handlers

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
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
	MaxImagePixels     = 40_000_000      // 40 MP decompression bomb guard
)

var allowedUploadMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
}

// ValidateUploadedImageBytes inspects the first 512 bytes via http.DetectContentType
// and validates the image header via image.DecodeConfig. Strictly permits JPEG, PNG, and GIF.
func ValidateUploadedImageBytes(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("file ảnh trống")
	}
	sniffLen := len(raw)
	if sniffLen > 512 {
		sniffLen = 512
	}
	detectedMime := http.DetectContentType(raw[:sniffLen])
	if !allowedUploadMimes[detectedMime] {
		return "", fmt.Errorf("file tải lên không phải ảnh hợp lệ (magic-byte MIME: %s); chỉ chấp nhận JPEG, PNG, GIF", detectedMime)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("cấu trúc header ảnh không hợp lệ hoặc bị hỏng")
	}
	if format != "jpeg" && format != "png" && format != "gif" {
		return "", fmt.Errorf("định dạng ảnh %s không được hỗ trợ", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return "", fmt.Errorf("kích thước ảnh không hợp lệ hoặc vượt ngưỡng an toàn")
	}
	return detectedMime, nil
}

// UploadHandler processes asynchronous CCCD image uploads strictly in memory (zero disk writes).
type UploadHandler struct {
	workerPool *worker.WorkerPool
}

// NewUploadHandler constructs an in-memory UploadHandler.
// The second parameter is retained for backward signature compatibility and ignored (zero disk writes).
func NewUploadHandler(wp *worker.WorkerPool, _ ...string) *UploadHandler {
	return &UploadHandler{
		workerPool: wp,
	}
}

// Upload POST /api/v1/cccd/upload
// @Summary Tải lên ảnh CCCD xử lý bất đồng bộ siêu tốc trong bộ nhớ (< 50ms, Zero-Disk)
// @Description Tiếp nhận ảnh CCCD mặt trước/mặt sau qua multipart/form-data. Kiểm tra magic-byte (512 bytes đầu + image.DecodeConfig), xử lý hoàn toàn trong RAM (không ghi xuống đĩa theo Nghị định 13/2023/NĐ-CP), tạo task_id và trả về 202 Accepted ngay lập tức. Kết quả trích xuất dạng chữ (không có sinh trắc học/khuôn mặt) được đẩy qua WebSocket.
// @Tags CCCD OCR
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param image formData file true "Ảnh CCCD/CMND (.jpg, .jpeg, .png, .gif)"
// @Success 202 {object} map[string]interface{} "Tiếp nhận tác vụ thành công"
// @Failure 400 {object} map[string]interface{} "File giả mạo, không hợp lệ hoặc quá dung lượng"
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

	// 2. Fast validation: declared size check
	if fileHeader.Size > MaxUploadSizeBytes {
		utils.BadRequest(c, fmt.Sprintf("Kích thước file vượt quá giới hạn tối đa (%d MB)", MaxUploadSizeBytes/(1024*1024)))
		return
	}

	// 3. Fast validation: extension check
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" {
		utils.BadRequest(c, "Định dạng file không được hỗ trợ (chỉ chấp nhận .jpg, .jpeg, .png, .gif)")
		return
	}

	// 4. Read multipart stream strictly into memory (Zero-Disk Policy)
	src, err := fileHeader.Open()
	if err != nil {
		utils.BadRequest(c, "Không thể đọc file ảnh tải lên")
		return
	}
	defer src.Close()

	rawBytes, err := io.ReadAll(io.LimitReader(src, MaxUploadSizeBytes+1))
	if err != nil {
		utils.BadRequest(c, "Không thể đọc dữ liệu file ảnh")
		return
	}
	if int64(len(rawBytes)) > MaxUploadSizeBytes {
		utils.BadRequest(c, fmt.Sprintf("Kích thước file vượt quá giới hạn tối đa (%d MB)", MaxUploadSizeBytes/(1024*1024)))
		return
	}

	// 5. Magic-Byte & Image Header Inspection (rejects spoofed extensions with HTTP 400)
	detectedMime, err := ValidateUploadedImageBytes(rawBytes)
	if err != nil {
		utils.BadRequest(c, err.Error())
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

	// 7. Enqueue in-memory task for Go Worker Pool
	taskID := uuid.New().String()
	task := models.OCRTask{
		TaskID:     taskID,
		UserID:     userID,
		Side:       side,
		ImageBytes: rawBytes,
		MimeType:   detectedMime,
		CreatedAt:  time.Now().UTC(),
	}

	if err := h.workerPool.Enqueue(task); err != nil {
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
