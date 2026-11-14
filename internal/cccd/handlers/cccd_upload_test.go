package handlers_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/cccd/handlers"
	"github.com/hskvinh/backend/internal/cccd/models"
	"github.com/hskvinh/backend/internal/cccd/worker"
	"github.com/hskvinh/backend/internal/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func makeValidJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 210, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("jpeg encode failed: %v", err)
	}
	return buf.Bytes()
}

func makeValidPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode failed: %v", err)
	}
	return buf.Bytes()
}

func setupUploadRouter(wp *worker.WorkerPool, tempDir string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(101))
		c.Set(middleware.ContextKeyUserRole, "candidate")
		c.Next()
	})
	h := handlers.NewUploadHandler(wp, tempDir)
	r.POST("/api/v1/cccd/upload", h.Upload)
	return r
}

func TestUpload_FastPath_Returns202Under50ms(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cccd_test_upload_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockEngine := &worker.MockOCREngine{}
	wp := worker.NewWorkerPool(mockEngine, 2, 100, nil, nil)

	r := setupUploadRouter(wp, tempDir)

	// Create valid multipart JPEG image
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "id_card_front.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	_, _ = part.Write(makeValidJPEGBytes(t))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()

	start := time.Now()
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("fast-path response took %v, exceeding 50ms constraint", elapsed)
	}

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			TaskID string           `json:"task_id"`
			Status models.OCRStatus `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !env.Success || env.Data.TaskID == "" {
		t.Fatalf("invalid response payload: %+v", env)
	}
	if env.Data.Status != models.StatusQueued {
		t.Fatalf("expected status QUEUED, got %s", env.Data.Status)
	}

	// Verify ZERO files were written to disk (Decree 13/2023 Zero-Disk Policy)
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("readdir error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero files written to disk, found %d files", len(entries))
	}
}

func TestUpload_MagicBytesAndNoDiskWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cccd_magic_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	wp := worker.NewWorkerPool(&worker.MockOCREngine{}, 2, 100, nil, nil)
	r := setupUploadRouter(wp, tempDir)

	// 1. Spoofed executable/script with .jpg extension -> must be rejected with HTTP 400
	var spoofBody bytes.Buffer
	mwSpoof := multipart.NewWriter(&spoofBody)
	spoofPart, _ := mwSpoof.CreateFormFile("image", "malicious_payload.jpg")
	_, _ = spoofPart.Write([]byte("MZ\x90\x00\x03\x00\x00\x00spoofed-pe-executable-content"))
	_ = mwSpoof.Close()

	reqSpoof := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", &spoofBody)
	reqSpoof.Header.Set("Content-Type", mwSpoof.FormDataContentType())
	wSpoof := httptest.NewRecorder()
	r.ServeHTTP(wSpoof, reqSpoof)
	if wSpoof.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for spoofed .jpg with PE magic bytes, got %d: %s", wSpoof.Code, wSpoof.Body.String())
	}

	// 2. Spoofed JPEG magic header with truncated/corrupted body (fails image.DecodeConfig) -> HTTP 400
	var corruptBody bytes.Buffer
	mwCorrupt := multipart.NewWriter(&corruptBody)
	corruptPart, _ := mwCorrupt.CreateFormFile("image", "corrupt_header.jpg")
	_, _ = corruptPart.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00})
	_ = mwCorrupt.Close()

	reqCorrupt := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", &corruptBody)
	reqCorrupt.Header.Set("Content-Type", mwCorrupt.FormDataContentType())
	wCorrupt := httptest.NewRecorder()
	r.ServeHTTP(wCorrupt, reqCorrupt)
	if wCorrupt.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for truncated JPEG failing DecodeConfig, got %d", wCorrupt.Code)
	}

	// 3. Valid PNG upload -> 202 Accepted and zero files written to disk
	var validBody bytes.Buffer
	mwValid := multipart.NewWriter(&validBody)
	validPart, _ := mwValid.CreateFormFile("image", "cccd_front.png")
	_, _ = validPart.Write(makeValidPNGBytes(t))
	_ = mwValid.Close()

	reqValid := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", &validBody)
	reqValid.Header.Set("Content-Type", mwValid.FormDataContentType())
	wValid := httptest.NewRecorder()
	r.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for valid PNG, got %d: %s", wValid.Code, wValid.Body.String())
	}

	entries, _ := os.ReadDir(tempDir)
	if len(entries) != 0 {
		t.Fatalf("expected zero disk writes in tempDir, found %d files", len(entries))
	}
}

func TestUpload_MissingFile_Returns400(t *testing.T) {
	wp := worker.NewWorkerPool(&worker.MockOCREngine{}, 2, 100, nil, nil)
	r := setupUploadRouter(wp, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}

func TestUpload_InvalidExtension_Returns400(t *testing.T) {
	wp := worker.NewWorkerPool(&worker.MockOCREngine{}, 2, 100, nil, nil)
	r := setupUploadRouter(wp, "")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("image", "document.pdf")
	part.Write([]byte("%PDF-1.4 mock"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for pdf, got %d", w.Code)
	}
}

