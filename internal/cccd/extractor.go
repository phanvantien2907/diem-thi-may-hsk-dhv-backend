package cccd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/cccd/entities"
)

// DefaultGeminiVisionModel is the default Google AI Studio multimodal model for OCR extraction.
const DefaultGeminiVisionModel = "gemini-3-flash-preview"

// IdCardExtractor defines the interface for OCR/LLM extraction providers.
type IdCardExtractor interface {
	Extract(ctx context.Context, image entities.PreparedImage, hints []string) (entities.RawExtraction, error)
}

// ModelNamedExtractor is an optional interface implemented by extractors that expose their active model identifier.
type ModelNamedExtractor interface {
	ModelName() string
}

var retryableHTTPStatuses = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	http.StatusGatewayTimeout:      true, // 504
}

// retiredGeminiModels maps legacy Google AI Studio model IDs that return 404 Not Found
// for new API keys to the active production model (gemini-3-flash-preview).
var retiredGeminiModels = map[string]bool{
	"gemini-2.0-flash":        true,
	"gemini-2.0-flash-exp":    true,
	"gemini-2.5-flash":        true,
	"gemini-2.5-flash-lite":   true,
	"gemini-1.5-flash":        true,
	"gemini-1.5-flash-latest": true,
	"gemini-1.5-pro":          true,
	"gemini-1.5-pro-latest":   true,
}

// defaultFallbackGeminiModels lists standard Google AI Studio Vision-capable models
// tried automatically if the configured model returns HTTP 404 Not Found.
var defaultFallbackGeminiModels = []string{
	"gemini-3-flash-preview",
	"gemini-3.1-flash-lite-preview",
	"gemini-flash-lite-latest",
	"gemini-2.0-flash",
	"gemini-2.5-flash",
}

// NormalizeGeminiModelName cleans whitespace, quotes, and accidental "models/" prefixes
// so endpoint URLs never become /v1beta/models/models/<name>:generateContent (404).
func NormalizeGeminiModelName(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.Trim(cleaned, "\"'`")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "models/")
	cleaned = strings.TrimSuffix(cleaned, ":generateContent")
	if cleaned == "" {
		return DefaultGeminiVisionModel
	}
	return cleaned
}

func resolveActiveModelName(raw string) string {
	norm := NormalizeGeminiModelName(raw)
	if retiredGeminiModels[norm] {
		return DefaultGeminiVisionModel
	}
	return norm
}

func buildCandidateModelList(primary string, fallbacks []string) []string {
	seen := make(map[string]struct{}, len(fallbacks)+1)
	out := make([]string, 0, len(fallbacks)+1)

	add := func(m string) {
		norm := NormalizeGeminiModelName(m)
		if norm == "" {
			return
		}
		if _, exists := seen[norm]; exists {
			return
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}

	add(primary)
	for _, fb := range fallbacks {
		add(fb)
	}
	return out
}

// GeminiExtractor calls the Google Gemini generateContent REST endpoint with structured JSON output.
type GeminiExtractor struct {
	mu             sync.RWMutex
	apiKey         string
	model          string
	fallbackModels []string
	maxRetries     int
	httpClient     *http.Client
	baseURL        string
}

// NewGeminiExtractor constructs a GeminiExtractor from Config.
func NewGeminiExtractor(cfg Config) (*GeminiExtractor, error) {
	if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
		return nil, errors.New("cccd: missing GEMINI_API_KEY")
	}
	model := resolveActiveModelName(cfg.GeminiModel)
	timeout := time.Duration(cfg.GeminiTimeoutSec * float64(time.Second))
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	retries := cfg.GeminiMaxRetries
	if retries < 0 {
		retries = 0
	}
	return &GeminiExtractor{
		apiKey:         strings.TrimSpace(cfg.GeminiAPIKey),
		model:          model,
		fallbackModels: buildCandidateModelList(model, defaultFallbackGeminiModels),
		maxRetries:     retries,
		httpClient:     &http.Client{Timeout: timeout},
		baseURL:        "https://generativelanguage.googleapis.com/v1beta/models",
	}, nil
}

// SetBaseURL overrides the Google AI Studio base URL (used in unit tests).
func (g *GeminiExtractor) SetBaseURL(baseURL string) {
	g.mu.Lock()
	g.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	g.mu.Unlock()
}

// SetHTTPClient overrides the HTTP client (used in unit tests).
func (g *GeminiExtractor) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	g.mu.Lock()
	g.httpClient = client
	g.mu.Unlock()
}

// ModelName returns the active Gemini model name used by this extractor.
func (g *GeminiExtractor) ModelName() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.model
}

func (g *GeminiExtractor) getCandidateModels() []string {
	g.mu.RLock()
	active := g.model
	fbs := g.fallbackModels
	g.mu.RUnlock()
	return buildCandidateModelList(active, fbs)
}

func (g *GeminiExtractor) setActiveModel(model string) {
	norm := NormalizeGeminiModelName(model)
	if norm == "" {
		return
	}
	g.mu.Lock()
	g.model = norm
	g.mu.Unlock()
}

// Extract sends the prepared image and prompt to Gemini and parses the structured RawExtraction JSON.
// If the configured model returns HTTP 404 Not Found on Google AI Studio, Extract automatically
// falls back through standard Gemini Vision models (e.g. gemini-2.0-flash, gemini-1.5-flash, gemini-1.5-pro).
func (g *GeminiExtractor) Extract(ctx context.Context, image entities.PreparedImage, hints []string) (entities.RawExtraction, error) {
	reqBody, err := g.buildRequestBody(image, hints)
	if err != nil {
		return entities.RawExtraction{}, NewPipelineError(ErrCodeInternal, "Không thể tạo request đọc giấy tờ", err)
	}

	candidates := g.getCandidateModels()
	var lastErr *PipelineError
	var lastStatus int

	for _, candidateModel := range candidates {
		if errors.Is(ctx.Err(), context.Canceled) {
			return entities.RawExtraction{}, context.Canceled
		}

		g.mu.RLock()
		baseURL := g.baseURL
		g.mu.RUnlock()

		endpoint := fmt.Sprintf("%s/%s:generateContent", baseURL, candidateModel)

		shouldTryNextModel := false
		for attempt := 0; attempt <= g.maxRetries; attempt++ {
			if errors.Is(ctx.Err(), context.Canceled) {
				return entities.RawExtraction{}, context.Canceled
			}

			rawText, statusCode, callErr := g.doCall(ctx, candidateModel, endpoint, reqBody)
			if callErr == nil {
				g.setActiveModel(candidateModel)
				return ParseRawExtractionJSON(rawText)
			}

			if errors.Is(callErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return entities.RawExtraction{}, context.Canceled
			}

			// If a model rejects system_instruction / response_mime_type with 400, try compatible Vision body once
			if statusCode == http.StatusBadRequest {
				if compatBody, compatErr := g.buildCompatibleRequestBody(image, hints); compatErr == nil {
					if compatText, compatStatus, compatCallErr := g.doCall(ctx, candidateModel, endpoint, compatBody); compatCallErr == nil {
						g.setActiveModel(candidateModel)
						return ParseRawExtractionJSON(compatText)
					} else {
						if errors.Is(compatCallErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
							return entities.RawExtraction{}, context.Canceled
						}
						statusCode = compatStatus
						callErr = compatCallErr
					}
				}
			}

			lastStatus = statusCode
			var pErr *PipelineError
			if errors.As(callErr, &pErr) {
				if pErr.Code == ErrCodeExtractionBadOutput {
					return entities.RawExtraction{}, pErr
				}
				lastErr = pErr
			} else {
				lastErr = NewPipelineError(ErrCodeExtractionFailed, "Không kết nối được dịch vụ đọc giấy tờ", callErr)
			}

			// On HTTP 404 (model not found on this endpoint/key), immediately advance to the next fallback model
			if statusCode == http.StatusNotFound || statusCode == http.StatusBadRequest {
				log.Printf("[cccd.extractor] model=%s returned status=%d; trying next fallback Gemini Vision model", candidateModel, statusCode)
				shouldTryNextModel = true
				break
			}

			if statusCode > 0 && !retryableHTTPStatuses[statusCode] {
				return entities.RawExtraction{}, lastErr
			}

			if attempt < g.maxRetries {
				multiplier := time.Duration(1 << attempt)
				waitDur := 1 * time.Second * multiplier
				if lastStatus == http.StatusTooManyRequests {
					waitDur = 3 * time.Second * multiplier
				}
				select {
				case <-ctx.Done():
					return entities.RawExtraction{}, context.Canceled
				case <-time.After(waitDur):
				}
			}
		}

		if !shouldTryNextModel {
			break
		}
	}

	if lastErr != nil {
		return entities.RawExtraction{}, lastErr
	}
	return entities.RawExtraction{}, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ đang lỗi hoặc quá tải")
}

func (g *GeminiExtractor) doCall(ctx context.Context, modelName string, endpoint string, bodyBytes []byte) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || (ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)) {
			log.Printf("[cccd.extractor] gemini request canceled by context (model=%s)", modelName)
			return "", 0, context.Canceled
		}
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			log.Printf("[cccd.extractor] gemini timeout (model=%s): %v", modelName, err)
			return "", 0, NewPipelineError(ErrCodeExtractionTimeout, "Không kết nối được dịch vụ đọc giấy tờ", err)
		}
		log.Printf("[cccd.extractor] gemini network error (model=%s): %v", modelName, err)
		return "", 0, NewPipelineError(ErrCodeExtractionFailed, "Không kết nối được dịch vụ đọc giấy tờ", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		errSnippet := strings.TrimSpace(string(respBytes))
		if len(errSnippet) > 300 {
			errSnippet = errSnippet[:300]
		}
		log.Printf("[cccd.extractor] gemini api error model=%s status=%d detail=%s", modelName, resp.StatusCode, errSnippet)
		return "", resp.StatusCode, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ đang lỗi hoặc quá tải")
	}

	var envelope struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return "", resp.StatusCode, NewPipelineError(ErrCodeExtractionBadOutput, "Kết quả đọc giấy tờ sai cấu trúc", err)
	}
	if len(envelope.Candidates) == 0 || len(envelope.Candidates[0].Content.Parts) == 0 {
		return "", resp.StatusCode, NewPipelineError(ErrCodeExtractionBadOutput, "Dịch vụ đọc giấy tờ trả về rỗng")
	}
	selectedText := ""
	for _, part := range envelope.Candidates[0].Content.Parts {
		if !part.Thought && strings.TrimSpace(part.Text) != "" {
			selectedText = part.Text
		}
	}
	if selectedText == "" {
		selectedText = envelope.Candidates[0].Content.Parts[0].Text
	}
	return selectedText, http.StatusOK, nil
}

func rawExtractionResponseSchema() map[string]any {
	nullableStr := map[string]any{"type": "STRING", "nullable": true}
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"is_id_card":          map[string]any{"type": "BOOLEAN"},
			"all_corners_visible": map[string]any{"type": "BOOLEAN"},
			"is_screen_photo":     map[string]any{"type": "BOOLEAN"},
			"is_photocopy":        map[string]any{"type": "BOOLEAN"},
			"is_blurry_or_glare":  map[string]any{"type": "BOOLEAN"},
			"card_title":          nullableStr,
			"side":                nullableStr,
			"id":                  nullableStr,
			"id_number":           nullableStr,
			"name":                nullableStr,
			"full_name":           nullableStr,
			"dob":                 nullableStr,
			"sex":                 nullableStr,
			"gender":              nullableStr,
			"nationality":         nullableStr,
			"home":                nullableStr,
			"address":             nullableStr,
			"address_province":    nullableStr,
			"address_district":    nullableStr,
			"address_ward":        nullableStr,
			"address_detail":      nullableStr,
			"address_entities": map[string]any{
				"type":     "OBJECT",
				"nullable": true,
				"properties": map[string]any{
					"province": nullableStr,
					"district": nullableStr,
					"ward":     nullableStr,
					"street":   nullableStr,
				},
			},
			"doe":        nullableStr,
			"ethnicity":  nullableStr,
			"religion":   nullableStr,
			"features":   nullableStr,
			"issue_date": nullableStr,
			"issue_loc":  nullableStr,
			"uncertain_fields": map[string]any{
				"type":  "ARRAY",
				"items": map[string]any{"type": "STRING"},
			},
		},
		"required": []string{
			"is_id_card",
			"all_corners_visible",
			"is_screen_photo",
			"is_photocopy",
			"is_blurry_or_glare",
			"id_number",
			"full_name",
			"dob",
			"gender",
			"ethnicity",
			"religion",
			"address_province",
			"address_district",
			"address_ward",
			"address_detail",
			"uncertain_fields",
		},
	}
}

func (g *GeminiExtractor) buildRequestBody(image entities.PreparedImage, hints []string) ([]byte, error) {
	b64 := base64.StdEncoding.EncodeToString(image.Data)
	payload := map[string]any{
		"system_instruction": map[string]any{
			"parts": []map[string]any{
				{"text": SystemPrompt},
			},
		},
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{
						"inline_data": map[string]any{
							"mime_type": image.Mime,
							"data":      b64,
						},
					},
					{
						"text": BuildUserPrompt(hints),
					},
				},
			},
		},
		"generation_config": map[string]any{
			"temperature":        0.0,
			"response_mime_type": "application/json",
			"response_schema":    rawExtractionResponseSchema(),
		},
	}
	return json.Marshal(payload)
}

// buildCompatibleRequestBody builds a universally compatible Gemini Vision generateContent request
// without top-level system_instruction in case an older endpoint rejects system_instruction.
func (g *GeminiExtractor) buildCompatibleRequestBody(image entities.PreparedImage, hints []string) ([]byte, error) {
	b64 := base64.StdEncoding.EncodeToString(image.Data)
	combinedPrompt := SystemPrompt + "\n\n" + BuildUserPrompt(hints)
	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{
						"inline_data": map[string]any{
							"mime_type": image.Mime,
							"data":      b64,
						},
					},
					{
						"text": combinedPrompt,
					},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature": 0.0,
		},
	}
	return json.Marshal(payload)
}

// cleanMarkdownJSON strips optional ```json ... ``` or ~~~json ... ~~~ fences returned by LLM responses.
func cleanMarkdownJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			trimmed = trimmed[idx+1:]
		}
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "```")
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "~~~")
		trimmed = strings.TrimSpace(trimmed)
	}
	if start := strings.Index(trimmed, "{"); start != -1 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			trimmed = trimmed[start : end+1]
		}
	}
	return trimmed
}

// ParseRawExtractionJSON parses and validates the raw JSON string from Gemini into RawExtraction,
// tolerantly filling missing booleans as false and missing slices as empty slices.
func ParseRawExtractionJSON(text string) (entities.RawExtraction, error) {
	trimmed := cleanMarkdownJSON(text)
	if trimmed == "" {
		return entities.RawExtraction{}, NewPipelineError(ErrCodeExtractionBadOutput, "Dịch vụ đọc giấy tờ trả về rỗng")
	}
	var out entities.RawExtraction
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return entities.RawExtraction{}, NewPipelineError(ErrCodeExtractionBadOutput, "Kết quả đọc giấy tờ sai cấu trúc", err)
	}
	if out.ID == nil && out.IDNumber != nil {
		out.ID = out.IDNumber
	} else if out.IDNumber == nil && out.ID != nil {
		out.IDNumber = out.ID
	}
	if out.Name == nil && out.FullName != nil {
		out.Name = out.FullName
	} else if out.FullName == nil && out.Name != nil {
		out.FullName = out.Name
	}
	if out.Sex == nil && out.Gender != nil {
		out.Sex = out.Gender
	} else if out.Gender == nil && out.Sex != nil {
		out.Gender = out.Sex
	}
	if out.AddressEntities.Province == nil && out.AddressProvince != nil {
		out.AddressEntities.Province = out.AddressProvince
	} else if out.AddressProvince == nil && out.AddressEntities.Province != nil {
		out.AddressProvince = out.AddressEntities.Province
	}
	if out.AddressEntities.District == nil && out.AddressDistrict != nil {
		out.AddressEntities.District = out.AddressDistrict
	} else if out.AddressDistrict == nil && out.AddressEntities.District != nil {
		out.AddressDistrict = out.AddressEntities.District
	}
	if out.AddressEntities.Ward == nil && out.AddressWard != nil {
		out.AddressEntities.Ward = out.AddressWard
	} else if out.AddressWard == nil && out.AddressEntities.Ward != nil {
		out.AddressWard = out.AddressEntities.Ward
	}
	if out.AddressEntities.Street == nil && out.AddressDetail != nil {
		out.AddressEntities.Street = out.AddressDetail
	} else if out.AddressDetail == nil && out.AddressEntities.Street != nil {
		out.AddressDetail = out.AddressEntities.Street
	}
	if out.UncertainFields == nil {
		out.UncertainFields = []string{}
	}
	return out, nil
}
