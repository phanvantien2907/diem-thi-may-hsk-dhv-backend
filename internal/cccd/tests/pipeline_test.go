package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/cccd"
	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/ws"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type fakeExtractor struct {
	mu      sync.Mutex
	results []entities.RawExtraction
	calls   [][]string
}

func newFakeExtractor(results ...entities.RawExtraction) *fakeExtractor {
	return &fakeExtractor{
		results: append([]entities.RawExtraction(nil), results...),
	}
}

func (f *fakeExtractor) Extract(_ context.Context, _ entities.PreparedImage, hints []string) (entities.RawExtraction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), hints...))
	if len(f.results) > 1 {
		out := f.results[0]
		f.results = f.results[1:]
		return out, nil
	}
	return f.results[0], nil
}

func testConfig() cccd.Config {
	cfg := cccd.DefaultConfig()
	cfg.GeminiAPIKey = "test-key"
	cfg.MinSharpness = 0.0
	return cfg
}

func makeJPEGBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	for i := 0; i < 4000; i++ {
		v := uint8(rng.Intn(256))
		img.SetRGBA(rng.Intn(width), rng.Intn(height), color.RGBA{R: v, G: v, B: v, A: 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode failed: %v", err)
	}
	return buf.Bytes()
}

func makeService(cfg cccd.Config, results ...entities.RawExtraction) (*cccd.Service, *fakeExtractor) {
	ex := newFakeExtractor(results...)
	svc := cccd.NewService(ex, cfg, func() time.Time { return testToday })
	return svc, ex
}

func TestPipeline_HappyPath(t *testing.T) {
	svc, ex := makeService(testConfig(), rawCCCDFront())
	resp := svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")
	if resp.ErrorCode != 0 || resp.ErrorMessage != "" {
		t.Fatalf("expected errorCode=0, got %d (%s)", resp.ErrorCode, resp.ErrorMessage)
	}
	if len(resp.Data) != 1 || resp.Data[0].Decision != cccd.DecisionAccept {
		t.Fatalf("expected 1 accepted item, got %+v", resp.Data)
	}
	if resp.RequestID == "" || len(ex.calls) != 1 || len(ex.calls[0]) != 0 {
		t.Fatalf("unexpected requestId or extractor calls: rid=%q calls=%v", resp.RequestID, ex.calls)
	}
}

func TestPipeline_NotAnIDCard(t *testing.T) {
	svc, _ := makeService(testConfig(), entities.RawExtraction{IsIDCard: false})
	resp := svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")
	if resp.ErrorCode != int(cccd.ErrCodeCardNotFound) || len(resp.Data) != 0 {
		t.Fatalf("expected ErrCodeCardNotFound and empty data, got code=%d len=%d", resp.ErrorCode, len(resp.Data))
	}
}

func TestPipeline_MissingCorners(t *testing.T) {
	svc, _ := makeService(testConfig(), rawCCCDFront(func(r *entities.RawExtraction) {
		r.AllCornersVisible = false
	}))
	resp := svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")
	if resp.ErrorCode != int(cccd.ErrCodeCardIncomplete) {
		t.Fatalf("expected ErrCodeCardIncomplete, got %d", resp.ErrorCode)
	}
}

func TestPipeline_ImageErrors(t *testing.T) {
	cfg := testConfig()
	svc, ex := makeService(cfg, rawCCCDFront())

	if resp := svc.Process(context.Background(), nil, ""); resp.ErrorCode != int(cccd.ErrCodeInvalidParams) {
		t.Fatalf("expected ErrCodeInvalidParams for empty bytes, got %d", resp.ErrorCode)
	}
	if resp := svc.Process(context.Background(), []byte("not an image at all"), ""); resp.ErrorCode != int(cccd.ErrCodeInvalidImage) {
		t.Fatalf("expected ErrCodeInvalidImage, got %d", resp.ErrorCode)
	}
	oversized := bytes.Repeat([]byte("x"), cfg.MaxUploadBytes+1)
	if resp := svc.Process(context.Background(), oversized, ""); resp.ErrorCode != int(cccd.ErrCodeFileTooLarge) {
		t.Fatalf("expected ErrCodeFileTooLarge, got %d", resp.ErrorCode)
	}
	if len(ex.calls) != 0 {
		t.Fatalf("expected 0 Gemini calls when image validation fails, got %d", len(ex.calls))
	}
}

func TestPipeline_AcceptsArbitraryResolutionsAndOrientations(t *testing.T) {
	cfg := testConfig()
	svc, ex := makeService(cfg, rawCCCDFront())

	// Test portrait / small / arbitrary dimensions like cmnd-test.jpeg (474x646), cmnd-test-2.jpeg (480x640), cmnd-test-3.jpeg (677x453), or 300x200
	testDims := [][2]int{
		{300, 200},
		{474, 646},
		{480, 640},
		{677, 453},
	}
	for _, dim := range testDims {
		w, h := dim[0], dim[1]
		resp := svc.Process(context.Background(), makeJPEGBytes(t, w, h), "")
		if resp.ErrorCode != 0 {
			t.Fatalf("expected resolution %dx%d to be accepted without ErrCodeImageTooSmall, got %d (%s)", w, h, resp.ErrorCode, resp.ErrorMessage)
		}
	}
	if len(ex.calls) != len(testDims) {
		t.Fatalf("expected %d extractor calls, got %d", len(testDims), len(ex.calls))
	}
}

func TestPipeline_SecondPassPicksBetterResult(t *testing.T) {
	bad := rawCCCDFront(func(r *entities.RawExtraction) {
		r.Sex = strPtr("Nữ") // conflicts with 4th digit of ID
	})
	good := rawCCCDFront()

	svc, ex := makeService(testConfig(), bad, good)
	resp := svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")

	if len(ex.calls) != 2 {
		t.Fatalf("expected 2 extractor calls, got %d", len(ex.calls))
	}
	if len(ex.calls[1]) != 2 || ex.calls[1][0] != "id" || ex.calls[1][1] != "sex" {
		t.Fatalf("expected second pass hints ['id', 'sex'], got %v", ex.calls[1])
	}
	if resp.Data[0].Sex.Value == nil || *resp.Data[0].Sex.Value != "Nam" || resp.Data[0].Decision != cccd.DecisionAccept {
		t.Fatalf("expected second pass to resolve sex='Nam' and decision='accept'")
	}
}

func TestPipeline_SecondPassKeepsFirstWhenNotBetter(t *testing.T) {
	bad := rawCCCDFront(func(r *entities.RawExtraction) {
		r.Sex = strPtr("Nữ")
	})
	svc, ex := makeService(testConfig(), bad, bad)
	resp := svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")
	if len(ex.calls) != 2 || resp.Data[0].Decision != cccd.DecisionReview {
		t.Fatalf("expected 2 calls and decision='review', got calls=%d decision=%s", len(ex.calls), resp.Data[0].Decision)
	}
}

func TestPipeline_SecondPassCanBeDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.SecondPassOnConflict = false
	bad := rawCCCDFront(func(r *entities.RawExtraction) {
		r.Sex = strPtr("Nữ")
	})
	svc, ex := makeService(cfg, bad)
	svc.Process(context.Background(), makeJPEGBytes(t, 900, 600), "")
	if len(ex.calls) != 1 {
		t.Fatalf("expected 1 call when SecondPassOnConflict=false, got %d", len(ex.calls))
	}
}

func TestHandler_ExtractAsync_Returns202AndPushesWebSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := ws.NewHub()
	go hub.Run()

	svc, _ := makeService(testConfig(), rawCCCDFront())
	svc.SetHub(hub)
	svc.StartWorkers(ctx)

	h := cccd.NewHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(777))
		c.Set(middleware.ContextKeyUserRole, "candidate")
		c.Next()
	})
	r.POST("/api/v1/cccd/extract", h.Extract)
	r.GET("/api/v1/cccd/jobs/:id", h.GetJobStatus)

	// Build multipart/form-data upload
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "cccd_front.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(makeJPEGBytes(t, 900, 600)); err != nil {
		t.Fatalf("part.Write failed: %v", err)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/extract", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted from POST /api/v1/cccd/extract, got %d: %s", w.Code, w.Body.String())
	}

	var acceptedEnv struct {
		Success bool `json:"success"`
		Data    struct {
			RequestID string `json:"request_id"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &acceptedEnv); err != nil {
		t.Fatalf("unmarshal 202 response failed: %v", err)
	}
	if !acceptedEnv.Success || acceptedEnv.Data.RequestID == "" {
		t.Fatalf("expected non-empty request_id in 202 response, got %+v", acceptedEnv)
	}

	// Wait for background worker to finish processing the job via polling endpoint
	// (Note: while status is "queued" or "processing", GetJob does not purge; once "completed", single-read auto-purge removes it!)
	deadline := time.Now().Add(2 * time.Second)
	var pollEnv struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
			Data   struct {
				ErrorCode int `json:"errorCode"`
				Data      []struct {
					ID struct {
						Value string `json:"value"`
					} `json:"id"`
					Decision string `json:"decision"`
				} `json:"data"`
			} `json:"data"`
		} `json:"data"`
	}

	// Another user must not see this job while it's in flight or completed
	if _, ok := svc.GetJob(acceptedEnv.Data.RequestID, 778); ok {
		t.Fatalf("expected job to be hidden from a different account")
	}

	completed := false
	for time.Now().Before(deadline) {
		pollReq := httptest.NewRequest(http.MethodGet, "/api/v1/cccd/jobs/"+acceptedEnv.Data.RequestID, nil)
		pw := httptest.NewRecorder()
		r.ServeHTTP(pw, pollReq)
		if pw.Code != http.StatusOK {
			t.Fatalf("expected 200 from GET /cccd/jobs/:id, got %d: %s", pw.Code, pw.Body.String())
		}
		if err := json.Unmarshal(pw.Body.Bytes(), &pollEnv); err != nil {
			t.Fatalf("unmarshal poll response failed: %v", err)
		}
		if pollEnv.Data.Status == "completed" {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatalf("timed out waiting for async job %s to complete", acceptedEnv.Data.RequestID)
	}
	if pollEnv.Data.Data.ErrorCode != 0 || len(pollEnv.Data.Data.Data) != 1 {
		t.Fatalf("unexpected poll payload: %+v", pollEnv)
	}
	if pollEnv.Data.Data.Data[0].ID.Value != "040203000001" {
		t.Fatalf("expected extracted ID '040203000001', got %q", pollEnv.Data.Data.Data[0].ID.Value)
	}

	// Single-read auto-purge: reading a completed job a second time must return 404 Not Found
	secondPollReq := httptest.NewRequest(http.MethodGet, "/api/v1/cccd/jobs/"+acceptedEnv.Data.RequestID, nil)
	pw2 := httptest.NewRecorder()
	r.ServeHTTP(pw2, secondPollReq)
	if pw2.Code != http.StatusNotFound {
		t.Fatalf("expected single-read auto-purge to return 404 on second read of completed job, got %d", pw2.Code)
	}
}

func TestHandler_Extract_MissingFileReturns400(t *testing.T) {
	svc, _ := makeService(testConfig(), rawCCCDFront())
	h := cccd.NewHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(777))
		c.Next()
	})
	r.POST("/api/v1/cccd/extract", h.Extract)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/extract", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when image is missing, got %d", w.Code)
	}
}

func TestHandler_GetJobStatus_UnknownReturns404(t *testing.T) {
	svc, _ := makeService(testConfig(), rawCCCDFront())
	h := cccd.NewHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(777))
		c.Next()
	})
	r.GET("/api/v1/cccd/jobs/:id", h.GetJobStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cccd/jobs/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", w.Code)
	}
}

func TestNormalizeGeminiModelName(t *testing.T) {
	if got := cccd.NormalizeGeminiModelName("models/gemini-2.0-flash"); got != "gemini-2.0-flash" {
		t.Fatalf("expected 'gemini-2.0-flash', got %q", got)
	}
	if got := cccd.NormalizeGeminiModelName(` "models/gemini-1.5-flash:generateContent" `); got != "gemini-1.5-flash" {
		t.Fatalf("expected 'gemini-1.5-flash', got %q", got)
	}
	if got := cccd.NormalizeGeminiModelName(""); got != cccd.DefaultGeminiVisionModel {
		t.Fatalf("expected default %q, got %q", cccd.DefaultGeminiVisionModel, got)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGeminiExtractor_FallbackOn404ToWorkingModel(t *testing.T) {
	var requestedPaths []string
	var lastPayload map[string]any
	var mu sync.Mutex

	mockClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var decoded map[string]any
			_ = json.NewDecoder(r.Body).Decode(&decoded)

			mu.Lock()
			requestedPaths = append(requestedPaths, r.URL.Path)
			lastPayload = decoded
			mu.Unlock()

			rec := httptest.NewRecorder()
			// Simulate 404 Not Found for non-existent/retired model
			if r.URL.Path == "/v1beta/models/non-existent-model:generateContent" {
				rec.WriteHeader(http.StatusNotFound)
				_, _ = rec.WriteString(`{"error":{"code":404,"message":"model not found"}}`)
				return rec.Result(), nil
			}

			// Succeed on fallback model (gemini-3-flash-preview) with Gemini 3 thought + JSON parts
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.WriteString(`{
				"candidates": [{
					"content": {
						"parts": [
							{
								"thought": true,
								"text": "Analyzing Vietnamese ID card..."
							},
							{
								"text": "~~~json\n{\"is_id_card\":true,\"all_corners_visible\":true,\"side\":\"front\",\"card_title\":\"CĂN CƯỚC CÔNG DÂN\",\"id\":\"040203000001\",\"name\":\"NGUYỄN VĂN A\",\"dob\":\"29/07/2003\",\"sex\":\"Nam\",\"nationality\":\"Việt Nam\"}\n~~~"
							}
						]
					}
				}]
			}`)
			return rec.Result(), nil
		}),
	}

	cfg := cccd.DefaultConfig()
	cfg.GeminiAPIKey = "test-api-key"
	cfg.GeminiModel = "models/non-existent-model"
	cfg.GeminiMaxRetries = 1

	ext, err := cccd.NewGeminiExtractor(cfg)
	if err != nil {
		t.Fatalf("NewGeminiExtractor error: %v", err)
	}
	ext.SetHTTPClient(mockClient)

	raw, err := ext.Extract(context.Background(), entities.PreparedImage{
		Data:   []byte("fake-jpeg"),
		Mime:   "image/jpeg",
		Width:  800,
		Height: 500,
	}, nil)
	if err != nil {
		t.Fatalf("expected fallback model to succeed after 404, got err: %v", err)
	}
	if !raw.IsIDCard || raw.ID == nil || *raw.ID != "040203000001" {
		t.Fatalf("unexpected extracted payload: %+v", raw)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestedPaths) < 2 {
		t.Fatalf("expected at least 2 requests (404 then fallback), got %v", requestedPaths)
	}
	if requestedPaths[0] != "/v1beta/models/non-existent-model:generateContent" {
		t.Fatalf("expected first request to non-existent-model, got %s", requestedPaths[0])
	}
	if requestedPaths[1] != "/v1beta/models/gemini-3-flash-preview:generateContent" {
		t.Fatalf("expected second request to fallback gemini-3-flash-preview, got %s", requestedPaths[1])
	}
	genCfg, ok := lastPayload["generation_config"].(map[string]any)
	if !ok || genCfg["response_mime_type"] != "application/json" || genCfg["response_schema"] == nil {
		t.Fatalf("expected generation_config with response_mime_type and response_schema, got %+v", lastPayload["generation_config"])
	}
}

func TestGeminiExtractor_AutoUpgradesRetiredModelsToGemini3Flash(t *testing.T) {
	var requestedPaths []string
	var mu sync.Mutex

	mockClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			requestedPaths = append(requestedPaths, r.URL.Path)
			mu.Unlock()

			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.WriteString(`{
				"candidates": [{
					"content": {
						"parts": [{
							"text": "{\"is_id_card\":true,\"all_corners_visible\":true,\"side\":\"front\",\"card_title\":\"CĂN CƯỚC CÔNG DÂN\",\"id\":\"040203000001\",\"name\":\"NGUYỄN VĂN A\",\"dob\":\"29/07/2003\",\"sex\":\"Nam\",\"nationality\":\"Việt Nam\",\"uncertain_fields\":[]}"
						}]
					}
				}]
			}`)
			return rec.Result(), nil
		}),
	}

	cfg := cccd.DefaultConfig()
	cfg.GeminiAPIKey = "test-api-key"
	cfg.GeminiModel = "gemini-2.5-flash" // retired model in legacy .env

	ext, err := cccd.NewGeminiExtractor(cfg)
	if err != nil {
		t.Fatalf("NewGeminiExtractor error: %v", err)
	}
	ext.SetHTTPClient(mockClient)

	_, err = ext.Extract(context.Background(), entities.PreparedImage{
		Data:   []byte("fake-jpeg"),
		Mime:   "image/jpeg",
		Width:  800,
		Height: 500,
	}, nil)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestedPaths) != 1 || requestedPaths[0] != "/v1beta/models/gemini-3-flash-preview:generateContent" {
		t.Fatalf("expected legacy gemini-2.5-flash to auto-upgrade to gemini-3-flash-preview on first call, got %v", requestedPaths)
	}
}

func TestPipeline_SHA256DeduplicationSavesTokens(t *testing.T) {
	svc, ex := makeService(testConfig(), rawCCCDFront())
	imgBytes := makeJPEGBytes(t, 900, 600)

	resp1 := svc.Process(context.Background(), imgBytes, "req-1")
	resp2 := svc.Process(context.Background(), imgBytes, "req-2")

	if resp1.ErrorCode != 0 || resp2.ErrorCode != 0 {
		t.Fatalf("expected both calls to succeed, got %d and %d", resp1.ErrorCode, resp2.ErrorCode)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("expected SHA-256 deduplication cache to invoke Gemini only once for identical image, got %d calls", len(ex.calls))
	}
}

func TestCacheKey_IncludesModelAndPromptVersion(t *testing.T) {
	imgBytes := makeJPEGBytes(t, 600, 400)
	baseKey := cccd.ComputeCacheKey(imgBytes, "gemini-3-flash-preview", cccd.PromptVersionHash(), true)

	// 1. Different model name must produce a distinct cache key
	diffModelKey := cccd.ComputeCacheKey(imgBytes, "gemini-2.5-pro", cccd.PromptVersionHash(), true)
	if baseKey == diffModelKey {
		t.Fatalf("expected different model name to produce a different cache key, got identical %s", baseKey)
	}

	// 2. Different prompt version hash must produce a distinct cache key
	diffPromptKey := cccd.ComputeCacheKey(imgBytes, "gemini-3-flash-preview", "prompt-v2-modified", true)
	if baseKey == diffPromptKey {
		t.Fatalf("expected different prompt version hash to produce a different cache key, got identical %s", baseKey)
	}

	// 3. Different second_pass_flag must produce a distinct cache key
	diffSecondPassKey := cccd.ComputeCacheKey(imgBytes, "gemini-3-flash-preview", cccd.PromptVersionHash(), false)
	if baseKey == diffSecondPassKey {
		t.Fatalf("expected different second_pass_flag to produce a different cache key, got identical %s", baseKey)
	}

	// 4. Deterministic: identical parameters must produce identical cache key
	sameKey := cccd.ComputeCacheKey(imgBytes, "gemini-3-flash-preview", cccd.PromptVersionHash(), true)
	if baseKey != sameKey {
		t.Fatalf("expected deterministic cache key %s, got %s", baseKey, sameKey)
	}
}

type blockingExtractor struct {
	started   chan struct{}
	cancelled chan error
}

func (b *blockingExtractor) Extract(ctx context.Context, _ entities.PreparedImage, _ []string) (entities.RawExtraction, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case b.cancelled <- ctx.Err():
	default:
	}
	return entities.RawExtraction{}, ctx.Err()
}

func TestWSDisconnect_CancelsOCRTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := ws.NewHub()
	go hub.Run()

	blockExt := &blockingExtractor{
		started:   make(chan struct{}, 1),
		cancelled: make(chan error, 1),
	}
	cfg := testConfig()
	svc := cccd.NewService(blockExt, cfg, func() time.Time { return testToday })
	svc.SetHub(hub)
	svc.StartWorkers(ctx)

	hub.AddOnUserDisconnected(func(userID int64) {
		svc.CancelUserTasks(userID)
	})

	targetUserID := int64(777)
	client := ws.NewClient(hub, nil, targetUserID, "candidate")
	time.Sleep(20 * time.Millisecond)

	if _, err := svc.EnqueueAsync(targetUserID, makeJPEGBytes(t, 600, 400), ""); err != nil {
		t.Fatalf("EnqueueAsync failed: %v", err)
	}

	// Wait for worker to start executing Extract
	select {
	case <-blockExt.started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for worker to begin OCR extraction")
	}

	// Disconnect the user's last WebSocket connection
	hub.UnregisterClient(client)

	// Verify the in-flight OCR context is immediately cancelled
	select {
	case err := <-blockExt.cancelled:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled on WS disconnect, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected in-flight OCR task to be cancelled when user's WebSocket disconnected")
	}
}

func TestGeminiExtractor_9DigitCMNDAnd12DigitCCCDExhaustiveSchema(t *testing.T) {
	// 1. Mock 9-digit CMND response: missing gender (null), includes ethnicity & religion with prefixes
	cmndJSON := `{
		"is_id_card": true,
		"all_corners_visible": true,
		"side": "front",
		"card_title": "GIẤY CHỨNG MINH NHÂN DÂN",
		"id_number": "123456789",
		"full_name": "TRẦN THỊ B",
		"dob": "24/09/1987",
		"gender": null,
		"ethnicity": "Dân tộc: Kinh",
		"religion": "Tôn giáo: Không",
		"home": "Hưng Nguyên, Nghệ An",
		"address": "Số 15 Quang Trung, P. Quang Trung, TP. Vinh, Nghệ An",
		"address_province": "Nghệ An",
		"address_district": "TP. Vinh",
		"address_ward": "P. Quang Trung",
		"address_detail": "Số 15 Quang Trung",
		"uncertain_fields": []
	}`
	rawCMND, err := cccd.ParseRawExtractionJSON(cmndJSON)
	if err != nil {
		t.Fatalf("ParseRawExtractionJSON for 9-digit CMND failed: %v", err)
	}
	if rawCMND.ID == nil || *rawCMND.ID != "123456789" {
		t.Fatalf("expected coalesced ID='123456789', got %v", rawCMND.ID)
	}
	if rawCMND.Sex != nil || rawCMND.Gender != nil {
		t.Fatalf("expected nil gender/sex for 9-digit CMND, got sex=%v gender=%v", rawCMND.Sex, rawCMND.Gender)
	}

	builtCMND, err := cccd.BuildCard(rawCMND, testToday)
	if err != nil {
		t.Fatalf("BuildCard for 9-digit CMND failed: %v", err)
	}
	if builtCMND.Data.Ethnicity.Value == nil || *builtCMND.Data.Ethnicity.Value != "Kinh" {
		t.Fatalf("expected Ethnicity='Kinh' preserved on 9-digit CMND, got %+v", builtCMND.Data.Ethnicity)
	}
	if builtCMND.Data.Religion.Value == nil || *builtCMND.Data.Religion.Value != "Không" {
		t.Fatalf("expected Religion='Không' preserved on 9-digit CMND, got %+v", builtCMND.Data.Religion)
	}
	if builtCMND.Data.Sex.Value != nil {
		t.Fatalf("expected nil Sex on 9-digit CMND, got %v", *builtCMND.Data.Sex.Value)
	}

	// 2. Mock 12-digit CCCD response: includes gender, ethnicity, religion, and address parts
	cccdJSON := `{
		"is_id_card": true,
		"all_corners_visible": true,
		"side": "front",
		"card_title": "CĂN CƯỚC CÔNG DÂN",
		"id_number": "040087000001",
		"full_name": "LÊ VĂN C",
		"dob": "24/09/1987",
		"gender": "Nam",
		"ethnicity": "Kinh",
		"religion": "Không",
		"nationality": "Việt Nam",
		"home": "Hưng Nguyên, Nghệ An",
		"address": "P. Hà Huy Tập, TP. Vinh, Nghệ An",
		"address_province": "Nghệ An",
		"address_district": "TP. Vinh",
		"address_ward": "P. Hà Huy Tập",
		"address_detail": "",
		"doe": "24/09/2027",
		"uncertain_fields": []
	}`
	rawCCCD, err := cccd.ParseRawExtractionJSON(cccdJSON)
	if err != nil {
		t.Fatalf("ParseRawExtractionJSON for 12-digit CCCD failed: %v", err)
	}
	builtCCCD, err := cccd.BuildCard(rawCCCD, testToday)
	if err != nil {
		t.Fatalf("BuildCard for 12-digit CCCD failed: %v", err)
	}
	if builtCCCD.Data.Sex.Value == nil || *builtCCCD.Data.Sex.Value != "Nam" {
		t.Fatalf("expected Sex='Nam' on 12-digit CCCD, got %+v", builtCCCD.Data.Sex)
	}
	if builtCCCD.Data.Ethnicity.Value == nil || *builtCCCD.Data.Ethnicity.Value != "Kinh" {
		t.Fatalf("expected Ethnicity='Kinh' on 12-digit CCCD, got %+v", builtCCCD.Data.Ethnicity)
	}
	if builtCCCD.Data.Religion.Value == nil || *builtCCCD.Data.Religion.Value != "Không" {
		t.Fatalf("expected Religion='Không' on 12-digit CCCD, got %+v", builtCCCD.Data.Religion)
	}
}

type gatedExtractor struct {
	started chan struct{}
	release chan struct{}
	raw     entities.RawExtraction
}

func (g *gatedExtractor) Extract(_ context.Context, _ entities.PreparedImage, _ []string) (entities.RawExtraction, error) {
	select {
	case g.started <- struct{}{}:
	default:
	}
	<-g.release
	return g.raw, nil
}

// H-03: Verify User B cannot hijack User A's in-flight job by supplying the same X-Request-ID,
// and GetJob rejects userID <= 0.
func TestService_H03_CrossUserRequestIDHijackAndZeroUserID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gated := &gatedExtractor{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
		raw:     rawCCCDFront(),
	}
	cfg := testConfig()
	cfg.MaxConcurrency = 1
	svc := cccd.NewService(gated, cfg, func() time.Time { return testToday })
	svc.StartWorkers(ctx)

	imgBytes := makeJPEGBytes(t, 600, 400)

	// 1. User A (777) enqueues with client-supplied requestID "shared-req-id"
	recA, err := svc.EnqueueAsync(777, imgBytes, "shared-req-id")
	if err != nil {
		t.Fatalf("User A EnqueueAsync failed: %v", err)
	}

	// Wait for User A's worker to start processing
	select {
	case <-gated.started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for User A job to start")
	}

	// 2. GetJob with userID <= 0 must be rejected
	if _, ok := svc.GetJob(recA.RequestID, 0); ok {
		t.Fatalf("expected GetJob with userID=0 to be rejected, but it succeeded")
	}
	if _, ok := svc.GetJob(recA.RequestID, -1); ok {
		t.Fatalf("expected GetJob with userID=-1 to be rejected, but it succeeded")
	}

	// 3. User B (888) attempts to collide with "shared-req-id" while User A's job is in flight
	recB, err := svc.EnqueueAsync(888, imgBytes, "shared-req-id")
	if err != nil {
		t.Fatalf("User B EnqueueAsync failed: %v", err)
	}
	if recB.RequestID == recA.RequestID {
		t.Fatalf("expected colliding requestID from User B to be regenerated, but both got %q", recB.RequestID)
	}

	// Release workers and verify User A still owns "shared-req-id"
	close(gated.release)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if jobA, ok := svc.GetJob(recA.RequestID, 777); ok && jobA.Status == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected User A (777) to retain ownership and retrieve completed job %q", recA.RequestID)
}

// H-05a: Verify GeminiExtractor never puts GEMINI_API_KEY in the URL query string (?key=)
// or leaks it in returned errors when a network failure occurs.
func TestGeminiExtractor_H05_DoesNotLeakAPIKeyInURLOrError(t *testing.T) {
	const secretAPIKey = "AIzaSySyntheticTestSecretKey987654"
	var capturedURL string
	var capturedHeaderKey string

	mockClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			capturedURL = r.URL.String()
			capturedHeaderKey = r.Header.Get("x-goog-api-key")
			return nil, context.DeadlineExceeded
		}),
	}

	cfg := cccd.DefaultConfig()
	cfg.GeminiAPIKey = secretAPIKey
	cfg.GeminiMaxRetries = 0

	ext, err := cccd.NewGeminiExtractor(cfg)
	if err != nil {
		t.Fatalf("NewGeminiExtractor failed: %v", err)
	}
	ext.SetHTTPClient(mockClient)

	_, extractErr := ext.Extract(context.Background(), entities.PreparedImage{
		Data:   []byte("fake-jpeg"),
		Mime:   "image/jpeg",
		Width:  600,
		Height: 400,
	}, nil)
	if extractErr == nil {
		t.Fatal("expected error from failing transport")
	}

	if strings.Contains(capturedURL, secretAPIKey) || strings.Contains(capturedURL, "key=") {
		t.Fatalf("expected request URL NOT to contain API key query parameter, got URL: %s", capturedURL)
	}
	if capturedHeaderKey != secretAPIKey {
		t.Fatalf("expected x-goog-api-key header to be set, got %q", capturedHeaderKey)
	}
	if strings.Contains(extractErr.Error(), secretAPIKey) {
		t.Fatalf("expected returned error NOT to contain secret API key, got: %v", extractErr)
	}
}

// H-06a: Verify Service.CancelUserTasks(0) does not cancel other users' active jobs.
func TestService_H06_CancelUserTasksZeroDoesNotCancelOtherUsers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blockExt := &blockingExtractor{
		started:   make(chan struct{}, 1),
		cancelled: make(chan error, 1),
	}
	svc := cccd.NewService(blockExt, testConfig(), func() time.Time { return testToday })
	svc.StartWorkers(ctx)

	if _, err := svc.EnqueueAsync(777, makeJPEGBytes(t, 600, 400), ""); err != nil {
		t.Fatalf("EnqueueAsync failed: %v", err)
	}
	select {
	case <-blockExt.started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for worker to start")
	}

	// Calling CancelUserTasks(0) or CancelUserTasks(-1) must be a no-op
	svc.CancelUserTasks(0)
	svc.CancelUserTasks(-1)

	select {
	case err := <-blockExt.cancelled:
		t.Fatalf("CancelUserTasks(0/-1) wrongly cancelled User 777's job: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Pass: User 777's job was not cancelled
	}
}

// H-08b: Verify EnqueueAsync cleans up s.jobs when s.jobQueue is full (no leaked job record).
func TestService_H08_EnqueueAsyncQueueFullDoesNotLeakJobRecord(t *testing.T) {
	// Do NOT start workers so jobQueue (capacity 128) fills up
	svc, _ := makeService(testConfig(), rawCCCDFront())
	imgBytes := makeJPEGBytes(t, 300, 200)

	for i := 0; i < 128; i++ {
		if _, err := svc.EnqueueAsync(777, imgBytes, ""); err != nil {
			t.Fatalf("enqueue %d failed unexpectedly: %v", i, err)
		}
	}

	// 129th enqueue must fail because queue is full
	_, err := svc.EnqueueAsync(777, imgBytes, "overflow-req-id")
	if err == nil {
		t.Fatal("expected 129th EnqueueAsync to fail when queue is full")
	}

	// "overflow-req-id" must NOT remain leaked in s.jobs
	if _, ok := svc.GetJob("overflow-req-id", 777); ok {
		t.Fatalf("expected failed EnqueueAsync (queue full) not to leak 'overflow-req-id' in s.jobs")
	}
}

// H-10: Verify Service.Process (?sync=true) respects ctx.Done() when semaphore s.sem is full.
func TestService_H10_ProcessRespectsContextWhenSemaphoreFull(t *testing.T) {
	blockExt := &blockingExtractor{
		started:   make(chan struct{}, 2),
		cancelled: make(chan error, 2),
	}
	cfg := testConfig()
	cfg.MaxConcurrency = 1
	svc := cccd.NewService(blockExt, cfg, func() time.Time { return testToday })

	imgBytes := makeJPEGBytes(t, 600, 400)
	imgBytes2 := makeJPEGBytes(t, 640, 420)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	go func() {
		_ = svc.Process(ctx1, imgBytes, "slot-holder")
	}()

	select {
	case <-blockExt.started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for first Process to occupy s.sem")
	}

	// Second Process call with a 30ms timeout while s.sem is full must return promptly without deadlocking
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()

	doneCh := make(chan *cccd.ErrorCode, 1)
	go func() {
		resp := svc.Process(ctx2, imgBytes2, "waiting-on-sem")
		code := cccd.ErrorCode(resp.ErrorCode)
		doneCh <- &code
	}()

	select {
	case code := <-doneCh:
		if *code != cccd.ErrCodeExtractionTimeout {
			t.Fatalf("expected ErrCodeExtractionTimeout when ctx expires waiting for s.sem, got %d", *code)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Service.Process blocked on s.sem ignoring ctx.Done()!")
	}
}

func TestGeminiExtractor_JPGUpload_NormalizesMimeTypeToImageJPEG(t *testing.T) {
	var capturedMimes []string
	var mu sync.Mutex

	mockClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode Gemini request payload: %v", err)
			}
			if contents, ok := body["contents"].([]any); ok && len(contents) > 0 {
				if firstContent, ok := contents[0].(map[string]any); ok {
					if parts, ok := firstContent["parts"].([]any); ok && len(parts) > 0 {
						if firstPart, ok := parts[0].(map[string]any); ok {
							if inlineData, ok := firstPart["inline_data"].(map[string]any); ok {
								if m, ok := inlineData["mime_type"].(string); ok {
									mu.Lock()
									capturedMimes = append(capturedMimes, m)
									mu.Unlock()
								}
							}
						}
					}
				}
			}

			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.WriteString(`{
				"candidates": [{
					"content": {
						"parts": [{
							"text": "{\"is_id_card\":true,\"all_corners_visible\":true,\"side\":\"front\",\"card_title\":\"CĂN CƯỚC CÔNG DÂN\",\"id\":\"040203000001\",\"name\":\"NGUYỄN VĂN A\",\"dob\":\"29/07/2003\",\"sex\":\"Nam\",\"nationality\":\"Việt Nam\",\"uncertain_fields\":[]}"
						}]
					}
				}]
			}`)
			return rec.Result(), nil
		}),
	}

	cfg := cccd.DefaultConfig()
	cfg.GeminiAPIKey = "AIzaSySyntheticTestKeyForJPGMimeCheck"
	cfg.GeminiMaxRetries = 0

	ext, err := cccd.NewGeminiExtractor(cfg)
	if err != nil {
		t.Fatalf("NewGeminiExtractor failed: %v", err)
	}
	ext.SetHTTPClient(mockClient)

	// 1. PrepareImage from valid .JPG bytes must output Mime = "image/jpeg"
	rawJPG := makeJPEGBytes(t, 640, 420)
	prepared, err := cccd.PrepareImage(rawJPG, cfg)
	if err != nil {
		t.Fatalf("PrepareImage failed on .JPG bytes: %v", err)
	}
	if prepared.Mime != "image/jpeg" {
		t.Fatalf("expected PrepareImage Mime to be 'image/jpeg', got %q", prepared.Mime)
	}

	if _, err := ext.Extract(context.Background(), prepared, nil); err != nil {
		t.Fatalf("GeminiExtractor.Extract failed on prepared .JPG: %v", err)
	}

	// 2. Even if PreparedImage has non-standard Mime ("image/jpg" or ".JPG"), Gemini payload must normalize to "image/jpeg"
	for _, nonStdMime := range []string{"image/jpg", "image/pjpeg", ".JPG", ".jpg", ""} {
		customPrepared := prepared
		customPrepared.Mime = nonStdMime
		if _, err := ext.Extract(context.Background(), customPrepared, nil); err != nil {
			t.Fatalf("GeminiExtractor.Extract failed with Mime=%q: %v", nonStdMime, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(capturedMimes) != 6 {
		t.Fatalf("expected 6 Gemini API calls, got %d", len(capturedMimes))
	}
	for i, m := range capturedMimes {
		if m != "image/jpeg" {
			t.Fatalf("call %d: expected Gemini inline_data.mime_type to be 'image/jpeg', got %q", i, m)
		}
	}
}

func TestHandler_ExtractEndpoint_AcceptsUppercaseJPG(t *testing.T) {
	svc, _ := makeService(testConfig(), rawCCCDFront())
	h := cccd.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/cccd/extract", func(c *gin.Context) {
		c.Set("user_id", int64(501))
		c.Next()
	}, h.Extract)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "CCCD_FRONT.JPG")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write(makeJPEGBytes(t, 640, 420))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cccd/extract?sync=true", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for .JPG extract?sync=true, got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			ErrorCode int `json:"errorCode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !env.Success || env.Data.ErrorCode != 0 {
		t.Fatalf("expected success with errorCode=0 for .JPG extract, got %s", w.Body.String())
	}
}
