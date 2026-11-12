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
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/cccd"
	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/cccd/enum"
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
	if resp := svc.Process(context.Background(), makeJPEGBytes(t, 300, 200), ""); resp.ErrorCode != int(cccd.ErrCodeImageTooSmall) {
		t.Fatalf("expected ErrCodeImageTooSmall, got %d", resp.ErrorCode)
	}
	oversized := bytes.Repeat([]byte("x"), cfg.MaxUploadBytes+1)
	if resp := svc.Process(context.Background(), oversized, ""); resp.ErrorCode != int(cccd.ErrCodeFileTooLarge) {
		t.Fatalf("expected ErrCodeFileTooLarge, got %d", resp.ErrorCode)
	}
	if len(ex.calls) != 0 {
		t.Fatalf("expected 0 Gemini calls when image validation fails, got %d", len(ex.calls))
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

	// Wait for background worker to finish processing the job
	deadline := time.Now().Add(2 * time.Second)
	var completedJob *entities.AsyncJobRecord
	for time.Now().Before(deadline) {
		job, ok := svc.GetJob(acceptedEnv.Data.RequestID, 777)
		if ok && job.Status == enum.JobStatusCompleted && job.Result != nil {
			completedJob = job
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completedJob == nil {
		t.Fatalf("timed out waiting for async job %s to complete", acceptedEnv.Data.RequestID)
	}
	if completedJob.ErrorCode != 0 || completedJob.Result.Data.ID.Value == nil {
		t.Fatalf("unexpected job result: %+v", completedJob)
	}
	if *completedJob.Result.Data.ID.Value != "040203012345" {
		t.Fatalf("expected extracted ID '040203012345', got %v", *completedJob.Result.Data.ID.Value)
	}

	// Polling endpoint must return the mapped DTO with FPT-compatible payload
	pollReq := httptest.NewRequest(http.MethodGet, "/api/v1/cccd/jobs/"+acceptedEnv.Data.RequestID, nil)
	pw := httptest.NewRecorder()
	r.ServeHTTP(pw, pollReq)
	if pw.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /cccd/jobs/:id, got %d: %s", pw.Code, pw.Body.String())
	}
	var pollEnv struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
			Data   struct {
				ErrorCode int `json:"errorCode"`
				Data      []struct {
					Decision string `json:"decision"`
				} `json:"data"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(pw.Body.Bytes(), &pollEnv); err != nil {
		t.Fatalf("unmarshal poll response failed: %v", err)
	}
	if pollEnv.Data.Status != "completed" || pollEnv.Data.Data.ErrorCode != 0 || len(pollEnv.Data.Data.Data) != 1 {
		t.Fatalf("unexpected poll payload: %s", pw.Body.String())
	}

	// Another user must not see this job
	if _, ok := svc.GetJob(acceptedEnv.Data.RequestID, 778); ok {
		t.Fatalf("expected job to be hidden from a different account")
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
								"text": "~~~json\n{\"is_id_card\":true,\"all_corners_visible\":true,\"side\":\"front\",\"card_title\":\"CĂN CƯỚC CÔNG DÂN\",\"id\":\"040203012345\",\"name\":\"PHAN VĂN TIẾN\",\"dob\":\"29/07/2003\",\"sex\":\"Nam\",\"nationality\":\"Việt Nam\"}\n~~~"
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
	if !raw.IsIDCard || raw.ID == nil || *raw.ID != "040203012345" {
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
							"text": "{\"is_id_card\":true,\"all_corners_visible\":true,\"side\":\"front\",\"card_title\":\"CĂN CƯỚC CÔNG DÂN\",\"id\":\"040203012345\",\"name\":\"PHAN VĂN TIẾN\",\"dob\":\"29/07/2003\",\"sex\":\"Nam\",\"nationality\":\"Việt Nam\",\"uncertain_fields\":[]}"
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

func TestGeminiExtractor_9DigitCMNDAnd12DigitCCCDExhaustiveSchema(t *testing.T) {
	// 1. Mock 9-digit CMND response: missing gender (null), includes ethnicity & religion with prefixes
	cmndJSON := `{
		"is_id_card": true,
		"all_corners_visible": true,
		"side": "front",
		"card_title": "GIẤY CHỨNG MINH NHÂN DÂN",
		"id_number": "188020231",
		"full_name": "HOÀNG MINH TRUNG",
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
	if rawCMND.ID == nil || *rawCMND.ID != "188020231" {
		t.Fatalf("expected coalesced ID='188020231', got %v", rawCMND.ID)
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
		"id_number": "040087001234",
		"full_name": "HOÀNG MINH TRUNG",
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
