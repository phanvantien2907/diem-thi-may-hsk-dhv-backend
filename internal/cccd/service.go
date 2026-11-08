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
	defaultJobTTL   = 10 * time.Minute
	defaultCacheTTL = 15 * time.Minute
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

// Service orchestrates image preprocessing, 2-pass Gemini extraction, cross-validation,
// and asynchronous worker execution with real-time WebSocket push notifications.
type Service struct {
	extractor IdCardExtractor
	cfg       Config
	todayFn   func() time.Time
	sem       chan struct{}
	hub       *ws.Hub

	jobQueue chan asyncTask
	jobsMu   sync.RWMutex
	jobs     map[string]*entities.AsyncJobRecord

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
		extractor: extractor,
		cfg:       cfg,
		todayFn:   todayFn,
		sem:       make(chan struct{}, cfg.MaxConcurrency),
		jobQueue:  make(chan asyncTask, 128),
		jobs:      make(map[string]*entities.AsyncJobRecord),
		cache:     make(map[string]cachedBuildResult),
		inflight:  make(map[string]*inflightExtraction),
	}
}

// Config returns the service's active configuration.
func (s *Service) Config() Config {
	return s.cfg
}

// SetHub attaches the WebSocket Hub so completed async extraction jobs are pushed to the user in real time.
func (s *Service) SetHub(hub *ws.Hub) {
	s.hub = hub
}

// StartWorkers launches background goroutine workers that process queued CCCD extraction jobs.
func (s *Service) StartWorkers(ctx context.Context) {
	workerCount := s.cfg.MaxConcurrency
	if workerCount <= 0 {
		workerCount = 2
	}
	for i := 0; i < workerCount; i++ {
		go s.workerLoop(ctx)
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

			started := time.Now()
			result, err := s.run(jobCtx, task.imageBytes)
			elapsed := time.Since(started).Milliseconds()
			cancel()

			now := time.Now().UTC()
			var wsPayload *response.IdCardResponseDTO

			s.jobsMu.Lock()
			if rec, ok := s.jobs[task.requestID]; ok {
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
	if requestID == "" {
		requestID = generateRequestID()
	}

	now := time.Now().UTC()
	rec := &entities.AsyncJobRecord{
		RequestID: requestID,
		UserID:    userID,
		Status:    enum.JobStatusProcessing,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.jobsMu.Lock()
	s.sweepExpiredJobsLocked(now)
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
		return nil, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ đang lỗi hoặc quá tải")
	}
}

// GetJob retrieves an AsyncJobRecord by requestID, enforcing user ownership when userID > 0.
func (s *Service) GetJob(requestID string, userID int64) (*entities.AsyncJobRecord, bool) {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()

	rec, exists := s.jobs[requestID]
	if !exists {
		return nil, false
	}
	if userID > 0 && rec.UserID > 0 && rec.UserID != userID {
		return nil, false
	}
	copyRec := *rec
	return &copyRec, true
}

func (s *Service) sweepExpiredJobsLocked(now time.Time) {
	for id, rec := range s.jobs {
		if now.Sub(rec.CreatedAt) > defaultJobTTL {
			delete(s.jobs, id)
		}
	}
}

// Process executes the full ID card extraction and cross-validation pipeline synchronously.
// Never logs PII (ID numbers, names, birth dates, or addresses).
func (s *Service) Process(ctx context.Context, imageBytes []byte, requestID string) *response.IdCardResponseDTO {
	if requestID == "" {
		requestID = generateRequestID()
	}
	started := time.Now()

	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	result, err := s.run(ctx, imageBytes)
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

func (s *Service) run(ctx context.Context, imageBytes []byte) (*entities.BuildResult, error) {
	prepared, err := PrepareImage(imageBytes, s.cfg)
	if err != nil {
		return nil, err
	}
	if s.extractor == nil {
		return nil, NewPipelineError(ErrCodeExtractionFailed, "Dịch vụ đọc giấy tờ chưa được cấu hình")
	}

	hashSum := sha256.Sum256(imageBytes)
	cacheKey := fmt.Sprintf("%x:%t", hashSum, s.cfg.SecondPassOnConflict)

	now := time.Now()
	s.dedupMu.Lock()
	for k, entry := range s.cache {
		if now.After(entry.expiresAt) {
			delete(s.cache, k)
		}
	}
	if cached, ok := s.cache[cacheKey]; ok && now.Before(cached.expiresAt) {
		s.dedupMu.Unlock()
		log.Printf("[cccd.pipeline] cache hit (sha256=%s) - saved Gemini API tokens", cacheKey[:12])
		return cached.result, nil
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
