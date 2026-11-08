package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	// Create multipart image
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "id_card_front.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	part.Write([]byte("fake-jpeg-image-bytes-content"))
	mw.Close()

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

	// Verify temporary file was saved on disk
	expectedFile := filepath.Join(tempDir, env.Data.TaskID+".jpg")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Fatalf("expected temp file %s to exist: %v", expectedFile, err)
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

