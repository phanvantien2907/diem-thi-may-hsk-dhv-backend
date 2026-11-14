package cccd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/cccd/dto/response"
	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/cccd/enum"
	"github.com/hskvinh/backend/internal/ws"
)

const (
	defaultJobTTL     = 3 * time.Minute // Strict max 3-minute TTL for ephemeral OCR results (Decree 13/2023)
	defaultCacheTTL   = 3 * time.Minute // Strict max 3-minute TTL for in-memory deduplication cache
	janitorTickPeriod = 60 * time.Second
)

type cachedBuildResult struct {
	result    *entities.BuildResult
	expiresAt time.Time
}

type inflightExtraction struct {
	done   chan struct{}
	result *entities.BuildResult
	err    error
}

type asyncTask struct {
	userID     int64
	requestID  string
	imageBytes []byte
}

type activeJobCancel struct {
	userID int64
	cancel context.CancelFunc
}

// ComputeCacheKey computes a versioned SHA-256 cache key over the raw image bytes,
// the active Gemini model name, the prompt version hash, and the second-pass flag:
// cache_key = sha256(image_bytes + model_name + prompt_version_hash + second_pass_flag).
func ComputeCacheKey(imageBytes []byte, modelName string, promptVersionHash string, secondPassFlag bool) string {
	h := sha256.New()
	h.Write(imageBytes)
	h.Write([]byte("|model=" + NormalizeGeminiModelName(modelName)))
	if promptVersionHash == "" {
		promptVersionHash = PromptVersionHash()
	}
	h.Write([]byte("|prompt=" + promptVersionHash))
	if secondPassFlag {
		h.Write([]byte("|second_pass=true"))
	} else {
		h.Write([]byte("|second_pass=false"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Service orchestrates image preprocessing, 2-pass Gemini extraction, cross-validation,
// and asynchronous worker execution with real-time WebSocket push notifications.
type Service struct {
	extractor         IdCardExtractor
	cfg               Config
	promptVersionHash string
	todayFn           func() time.Time
	sem               chan struct{}
	hub               *ws.Hub

	jobQueue chan asyncTask
	jobsMu   sync.RWMutex
	jobs     map[string]*entities.AsyncJobRecord

	cancelsMu     sync.Mutex
	activeCancels map[string]activeJobCancel

	dedupMu  sync.Mutex
	cache    map[string]cachedBuildResult
	inflight map[string]*inflightExtraction
}

// NewService constructs an IdCardService with a concurrency semaphore and async job queue.
func NewService(extractor IdCardExtractor, cfg Config, todayFn func() time.Time) *Service {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}
	if todayFn == nil {
		todayFn = func() time.Time {
			now := time.Now().UTC()
			return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	return &Service{
		extractor:         extractor,
		cfg:               cfg,
		promptVersionHash: PromptVersionHash(),
		todayFn:           todayFn,
		sem:               make(chan struct{}, cfg.MaxConcurrency),
		jobQueue:          make(chan asyncTask, 128),
		jobs:              make(map[string]*entities.AsyncJobRecord),
		activeCancels:     make(map[string]activeJobCancel),
		cache:             make(map[string]cachedBuildResult),
		inflight:          make(map[string]*inflightExtraction),
	}
}

// SetPromptVersionHash overrides the active prompt version hash used for cache key computation.
func (s *Service) SetPromptVersionHash(hash string) {
	s.dedupMu.Lock()
	s.promptVersionHash = hash
	s.dedupMu.Unlock()
}

// Config returns the service's active configuration.
func (s *Service) Config() Config {
	return s.cfg
}

// SetHub attaches the WebSocket Hub so completed async extraction jobs are pushed to the user in real time,
// and registers a disconnect hook to cancel in-flight OCR tasks when the user disconnects.
func (s *Service) SetHub(hub *ws.Hub) {
	s.hub = hub
	if hub != nil {
		hub.AddOnUserDisconnected(s.CancelUserTasks)
	}
}

// StartWorkers launches background goroutine workers and the 60s background janitor ticker.
func (s *Service) StartWorkers(ctx context.Context) {
	workerCount := s.cfg.MaxConcurrency
	if workerCount <= 0 {
		workerCount = 2
	}
	for i := 0; i < workerCount; i++ {
		go s.workerLoop(ctx)
	}
	go s.janitorLoop(ctx)
}

func (s *Service) janitorLoop(ctx context.Context) {
	ticker := time.NewTicker(janitorTickPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.evictExpired(now.UTC())
		}
	}
}

func (s *Service) evictExpired(now time.Time) {
	s.jobsMu.Lock()
	for id, rec := range s.jobs {
		if now.Sub(rec.CreatedAt) > defaultJobTTL {
			delete(s.jobs, id)
		}
	}
	s.jobsMu.Unlock()

	s.dedupMu.Lock()
	for k, entry := range s.cache {
		if now.After(entry.expiresAt) {
			delete(s.cache, k)
		}
	}
	s.dedupMu.Unlock()
}

// CancelUserTasks cancels all in-flight asynchronous OCR tasks belonging to a positive userID.
func (s *Service) CancelUserTasks(userID int64) {
	if userID <= 0 {
		return
	}
	s.cancelsMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.activeCancels))
	for reqID, entry := range s.activeCancels {
		if entry.userID == userID {
			if entry.cancel != nil {
				cancels = append(cancels, entry.cancel)
			}
			delete(s.activeCancels, reqID)
		}
	}
	s.cancelsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Service) workerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.jobQueue:
			timeoutSec := int(s.cfg.GeminiTimeoutSec*2) + 20
			if timeoutSec < 60 {
				timeoutSec = 60
			}
			jobCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)

			s.cancelsMu.Lock()
			s.activeCancels[task.requestID] = activeJobCancel{
				userID: task.userID,
				cancel: cancel,
			}
			s.cancelsMu.Unlock()

			started := time.Now()
			result, err := s.safeRun(jobCtx, task.imageBytes)
			elapsed := time.Since(started).Milliseconds()
			cancel()

			// Zero out in-memory image slice immediately after processing
			for i := range task.imageBytes {
				task.imageBytes[i] = 0
			}
			task.imageBytes = nil

			s.cancelsMu.Lock()
			delete(s.activeCancels, task.requestID)
			s.cancelsMu.Unlock()

			now := time.Now().UTC()
			var wsPayload *response.IdCardResponseDTO

			s.jobsMu.Lock()
			if rec, ok := s.jobs[task.requestID]; ok && rec.UserID == task.userID {
				rec.UpdatedAt = now
				rec.CompletedAt = &now
				rec.ElapsedMs = elapsed
				if err != nil {
					rec.Status = enum.JobStatusFailed
					var pErr *PipelineError
					if errors.As(err, &pErr) {
						rec.ErrorCode = int(pErr.Code)
						msg := pErr.Message
						rec.Error = &msg
						wsPayload = ToErrorIdCardResponseDTO(pErr.Code, pErr.Message, task.requestID, elapsed)
					} else {
						rec.ErrorCode = int(ErrCodeInternal)
						msg := "Lỗi hệ thống"
						rec.Error = &msg
						wsPayload = ToErrorIdCardResponseDTO(ErrCodeInternal, "Lỗi hệ thống", task.requestID, elapsed)
					}
				} else {
					rec.Status = enum.JobStatusCompleted
					rec.Result = result
					rec.ErrorCode = int(ErrCodeOK)
					wsPayload = ToIdCardResponseDTO(result, task.requestID, elapsed)
				}
			}
			s.jobsMu.Unlock()

			if s.hub != nil && task.userID > 0 && wsPayload != nil {
				s.hub.SendToUser(task.userID, ws.EventCCCDExtracted, wsPayload)
			}
		}
	}
}

// EnqueueAsync performs fast pre-validation, registers an AsyncJobRecord, and pushes the task
// to the background worker queue so the HTTP handler can immediately return 202 Accepted.
func (s *Service) EnqueueAsync(userID int64, imageBytes []byte, requestID string) (*entities.AsyncJobRecord, error) {
	if len(imageBytes) == 0 {
		return nil, NewPipelineError(ErrCodeInvalidParams, "Thiếu file ảnh trong request")
	}
	if s.cfg.MaxUploadBytes > 0 && len(imageBytes) > s.cfg.MaxUploadBytes {
		mb := float64(s.cfg.MaxUploadBytes) / (1024.0 * 1024.0)
		return nil, NewPipelineError(ErrCodeFileTooLarge, fmt.Sprintf("Ảnh vượt quá %g MB", mb))
	}
	if _, _, err := ValidateImageMagicBytes(imageBytes); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.jobsMu.Lock()
	if requestID == "" {
		requestID = generateRequestID()
	} else if _, exists := s.jobs[requestID]; exists {
		// Prevent cross-user job hijack via duplicate/colliding X-Request-ID
		requestID = generateRequestID()
	}
	rec := &entities.AsyncJobRecord{
		RequestID: requestID,
		UserID:    userID,
		Status:    enum.JobStatusProcessing,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[requestID] = rec
	s.jobsMu.Unlock()

	imgCopy := append([]byte(nil), imageBytes...)
	select {
	case s.jobQueue <- asyncTask{
		userID:     userID,
		requestID:  requestID,
		imageBytes: imgCopy,
	}:
		return rec, nil
	default:
		s.jobsMu.Lock()
		delete(s.jobs, requestID)
		s.jobsMu.Unlock()
		return nil, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ đang lỗi hoặc quá tải")
	}
}

// GetJob retrieves an AsyncJobRecord by requestID, strictly enforcing positive user ownership,
// O(1) lazy expiration (max 3-minute TTL), and single-read auto-purge for completed/failed jobs.
func (s *Service) GetJob(requestID string, userID int64) (*entities.AsyncJobRecord, bool) {
	now := time.Now().UTC()
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	rec, exists := s.jobs[requestID]
	if !exists {
		return nil, false
	}
	if now.Sub(rec.CreatedAt) > defaultJobTTL {
		delete(s.jobs, requestID)
		return nil, false
	}
	if userID <= 0 || rec.UserID <= 0 || rec.UserID != userID {
		return nil, false
	}
	copyRec := *rec
	if rec.Status == enum.JobStatusCompleted || rec.Status == enum.JobStatusFailed {
		delete(s.jobs, requestID)
	}
	return &copyRec, true
}

// Process executes the full ID card extraction and cross-validation pipeline synchronously.
// Never logs PII (ID numbers, names, birth dates, or addresses).
func (s *Service) Process(ctx context.Context, imageBytes []byte, requestID string) *response.IdCardResponseDTO {
	if requestID == "" {
		requestID = generateRequestID()
	}
	started := time.Now()

	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		elapsed := time.Since(started).Milliseconds()
		return ToErrorIdCardResponseDTO(ErrCodeExtractionTimeout, "Không kết nối được dịch vụ đọc giấy tờ", requestID, elapsed)
	}
	defer func() { <-s.sem }()

	result, err := s.safeRun(ctx, imageBytes)
	elapsed := time.Since(started).Milliseconds()

	if err != nil {
		var pErr *PipelineError
		if errors.As(err, &pErr) {
			log.Printf("[cccd.pipeline] rid=%s error code=%d elapsed_ms=%d", requestID, pErr.Code, elapsed)
			return ToErrorIdCardResponseDTO(pErr.Code, pErr.Message, requestID, elapsed)
		}
		log.Printf("[cccd.pipeline] rid=%s unexpected error elapsed_ms=%d", requestID, elapsed)
		return ToErrorIdCardResponseDTO(ErrCodeInternal, "Lỗi hệ thống", requestID, elapsed)
	}

	log.Printf("[cccd.pipeline] rid=%s ok decision=%s type=%s elapsed_ms=%d",
		requestID, result.Data.Decision, result.Data.TypeNew, elapsed)

	return ToIdCardResponseDTO(result, requestID, elapsed)
}

func (s *Service) activeModelName() string {
	if mn, ok := s.extractor.(ModelNamedExtractor); ok {
		if m := mn.ModelName(); m != "" {
			return m
		}
	}
	return NormalizeGeminiModelName(s.cfg.GeminiModel)
}

func (s *Service) safeRun(ctx context.Context, imageBytes []byte) (res *entities.BuildResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[cccd.pipeline] recovered panic during extraction: %v", r)
			res = nil
			err = NewPipelineError(ErrCodeInternal, "Lỗi hệ thống")
		}
	}()
	return s.run(ctx, imageBytes)
}

func (s *Service) run(ctx context.Context, imageBytes []byte) (*entities.BuildResult, error) {
	prepared, err := PrepareImage(imageBytes, s.cfg)
	if err != nil {
		return nil, err
	}
	if s.extractor == nil {
		return nil, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ chưa được cấu hình")
	}

	now := time.Now()
	s.dedupMu.Lock()
	cacheKey := ComputeCacheKey(imageBytes, s.activeModelName(), s.promptVersionHash, s.cfg.SecondPassOnConflict)

	// O(1) lazy expiration check on the requested key (no full-map loop on hot path)
	if cached, ok := s.cache[cacheKey]; ok {
		if now.Before(cached.expiresAt) {
			s.dedupMu.Unlock()
			log.Printf("[cccd.pipeline] cache hit (sha256=%s) - saved Gemini API tokens", cacheKey[:12])
			return cached.result, nil
		}
		delete(s.cache, cacheKey)
	}
	if flight, ok := s.inflight[cacheKey]; ok {
		s.dedupMu.Unlock()
		log.Printf("[cccd.pipeline] joining in-flight extraction (sha256=%s) - saved duplicate Gemini API call", cacheKey[:12])
		select {
		case <-ctx.Done():
			return nil, NewPipelineError(ErrCodeExtractionTimeout, "Không kết nối được dịch vụ đọc giấy tờ", ctx.Err())
		case <-flight.done:
			return flight.result, flight.err
		}
	}
	flight := &inflightExtraction{done: make(chan struct{})}
	s.inflight[cacheKey] = flight
	s.dedupMu.Unlock()

	defer func() {
		s.dedupMu.Lock()
		delete(s.inflight, cacheKey)
		if flight.err == nil && flight.result != nil {
			s.cache[cacheKey] = cachedBuildResult{
				result:    flight.result,
				expiresAt: time.Now().Add(defaultCacheTTL),
			}
		}
		close(flight.done)
		s.dedupMu.Unlock()
	}()

	result, extractErr := s.extractAndBuild(ctx, prepared)
	flight.result = result
	flight.err = extractErr
	return result, extractErr
}

func (s *Service) extractAndBuild(ctx context.Context, prepared entities.PreparedImage) (*entities.BuildResult, error) {
	raw, err := s.extractor.Extract(ctx, prepared, nil)
	if err != nil {
		return nil, err
	}
	if err := guardRawExtraction(raw); err != nil {
		return nil, err
	}

	today := s.todayFn()
	result, err := BuildCard(raw, today, prepared.Warnings...)
	if err != nil {
		return nil, err
	}

	// Second pass when cross-field conflicts exist (e.g., CCCD digit vs sex or birth year)
	if s.cfg.SecondPassOnConflict && len(result.Conflicts) > 0 {
		hints := make([]string, 0, len(result.Conflicts))
		for k := range result.Conflicts {
			hints = append(hints, k)
		}
		sort.Strings(hints)

		raw2, err2 := s.extractor.Extract(ctx, prepared, hints)
		if err2 == nil {
			if gErr := guardRawExtraction(raw2); gErr == nil {
				if result2, bErr := BuildCard(raw2, today, prepared.Warnings...); bErr == nil {
					if result2.Penalty < result.Penalty {
						result = result2
					}
				}
			}
		} else {
			var pErr *PipelineError
			if errors.As(err2, &pErr) {
				log.Printf("[cccd.pipeline] second pass skipped code=%d", pErr.Code)
			}
		}
	}

	return result, nil
}

func guardRawExtraction(raw entities.RawExtraction) error {
	if !raw.IsIDCard {
		return NewPipelineError(ErrCodeCardNotFound, "Không tìm thấy giấy tờ tùy thân trong ảnh")
	}
	if !raw.AllCornersVisible {
		return NewPipelineError(ErrCodeCardIncomplete, "Giấy tờ bị thiếu góc, vui lòng chụp đủ 4 góc")
	}
	return nil
}

func generateRequestID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
