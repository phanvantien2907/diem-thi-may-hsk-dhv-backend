package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/cccd/models"
	"github.com/hskvinh/backend/internal/location/matcher"
)

const (
	DefaultRedisChannel = "cccd:ocr_events"
	DefaultQueueSize    = 512
	DefaultConcurrency  = 4
	DefaultTaskTTL      = 3 * time.Minute // Strict max 3-minute TTL for ephemeral OCR state (Decree 13/2023)
	JanitorInterval     = 60 * time.Second
)

// RedisPublisher abstracts Redis Pub/Sub publishing.
type RedisPublisher interface {
	Publish(ctx context.Context, channel string, message any) error
}

// OCREngine defines the in-memory text extraction capability (zero disk writes).
// Strictly extracts textual fields only. NO BIOMETRICS OR FACIAL RECOGNITION.
type OCREngine interface {
	ExtractText(ctx context.Context, imageBytes []byte) (*models.OCRResultData, error)
}

// EventDispatcher is an optional callback to notify in-memory components (e.g. WebSocket Hub).
type EventDispatcher func(payload models.OCREventPayload)

type activeTaskCancel struct {
	userID int64
	cancel context.CancelFunc
}

// WorkerPool manages background worker goroutines that process OCR tasks asynchronously.
type WorkerPool struct {
	engine        OCREngine
	queue         chan models.OCRTask
	queueSlots    chan struct{}
	redisSvc      RedisPublisher
	dispatcher    EventDispatcher
	channel       string
	workers       int
	wg            sync.WaitGroup
	mu            sync.Mutex
	started       bool
	tasksMu       sync.RWMutex
	tasks         map[string]models.OCREventPayload
	cancelsMu     sync.Mutex
	activeCancels map[string]activeTaskCancel
}

// NewWorkerPool constructs a production-ready WorkerPool.
func NewWorkerPool(
	engine OCREngine,
	concurrency int,
	queueSize int,
	redisSvc RedisPublisher,
	dispatcher EventDispatcher,
) *WorkerPool {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}
	return &WorkerPool{
		engine:        engine,
		queue:         make(chan models.OCRTask, queueSize),
		queueSlots:    make(chan struct{}, queueSize),
		redisSvc:      redisSvc,
		dispatcher:    dispatcher,
		channel:       DefaultRedisChannel,
		workers:       concurrency,
		tasks:         make(map[string]models.OCREventPayload),
		activeCancels: make(map[string]activeTaskCancel),
	}
}

// SetChannel configures the Redis Pub/Sub channel name.
func (wp *WorkerPool) SetChannel(channel string) {
	if channel != "" {
		wp.channel = channel
	}
}

// Enqueue submits a task into the worker queue.
// Returns an error if the queue is saturated.
func (wp *WorkerPool) Enqueue(task models.OCRTask) error {
	select {
	case wp.queueSlots <- struct{}{}:
	default:
		return errors.New("ocr worker queue is full")
	}

	if task.Ctx == nil {
		task.Ctx, task.Cancel = context.WithCancel(context.Background())
	} else if task.Cancel == nil {
		task.Ctx, task.Cancel = context.WithCancel(task.Ctx)
	}
	if task.TaskID != "" {
		wp.cancelsMu.Lock()
		wp.activeCancels[task.TaskID] = activeTaskCancel{
			userID: task.UserID,
			cancel: task.Cancel,
		}
		wp.cancelsMu.Unlock()
	}

	// Emit initial QUEUED event BEFORE handing the task to worker goroutines
	// so PROCESSING / COMPLETED can never race ahead of QUEUED.
	wp.emitEvent(models.OCREventPayload{
		Event:     "ocr_progress",
		RequestID: task.TaskID,
		TaskID:    task.TaskID,
		AccountID: task.UserID,
		UserID:    task.UserID,
		Status:    models.StatusQueued,
		Progress:  10,
		Timestamp: time.Now().UTC(),
	})

	wp.queue <- task
	return nil
}

// Start launches the worker goroutines and the 60s background janitor ticker.
func (wp *WorkerPool) Start(ctx context.Context) {
	wp.mu.Lock()
	if wp.started {
		wp.mu.Unlock()
		return
	}
	wp.started = true
	wp.mu.Unlock()

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(ctx, i+1)
	}
	go wp.janitorLoop(ctx)
	log.Printf("[ocr_worker] started %d background workers for channel %s", wp.workers, wp.channel)
}

func (wp *WorkerPool) janitorLoop(ctx context.Context) {
	ticker := time.NewTicker(JanitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			wp.evictExpiredTasks(now.UTC())
		}
	}
}

func (wp *WorkerPool) evictExpiredTasks(now time.Time) {
	wp.tasksMu.Lock()
	defer wp.tasksMu.Unlock()
	for id, rec := range wp.tasks {
		if now.Sub(rec.Timestamp) > DefaultTaskTTL {
			delete(wp.tasks, id)
		}
	}
}

func (wp *WorkerPool) workerLoop(ctx context.Context, workerID int) {
	defer wp.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-wp.queue:
			if !ok {
				return
			}
			<-wp.queueSlots
			wp.processTask(ctx, task)
		}
	}
}

// CancelTask cancels an in-flight or queued OCR task by its taskID.
func (wp *WorkerPool) CancelTask(taskID string) {
	wp.cancelsMu.Lock()
	entry, ok := wp.activeCancels[taskID]
	delete(wp.activeCancels, taskID)
	wp.cancelsMu.Unlock()
	if ok && entry.cancel != nil {
		entry.cancel()
	}
}

// CancelUserTasks cancels all queued and in-flight OCR tasks belonging to a specific positive userID.
func (wp *WorkerPool) CancelUserTasks(userID int64) {
	if userID <= 0 {
		return
	}
	wp.cancelsMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(wp.activeCancels))
	for id, entry := range wp.activeCancels {
		if entry.userID == userID {
			if entry.cancel != nil {
				cancels = append(cancels, entry.cancel)
			}
			delete(wp.activeCancels, id)
		}
	}
	wp.cancelsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func sanitizeWorkerError(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	raw := err.Error()
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "http://") ||
		strings.Contains(lower, "https://") ||
		strings.Contains(lower, "key=") ||
		strings.Contains(raw, "AIza") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "local ocr execution error") ||
		strings.Contains(lower, "panic") {
		return fallback
	}
	return raw
}

func (wp *WorkerPool) processTask(ctx context.Context, task models.OCRTask) {
	// Zero-Disk Policy: clear in-memory image buffer immediately when worker finishes
	defer func() {
		for i := range task.ImageBytes {
			task.ImageBytes[i] = 0
		}
		task.ImageBytes = nil
	}()

	defer func() {
		wp.cancelsMu.Lock()
		delete(wp.activeCancels, task.TaskID)
		wp.cancelsMu.Unlock()
	}()

	if task.Cancel != nil {
		defer task.Cancel()
	}

	// Recover from any unexpected panic inside OCREngine so the worker goroutine stays alive
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ocr_worker] recovered panic in task %s: %v", task.TaskID, r)
			const safeMsg = "Dịch vụ bận, vui lòng thử lại hoặc nhập thủ công."
			wp.emitEvent(models.OCREventPayload{
				Event:     "ocr_failed",
				RequestID: task.TaskID,
				TaskID:    task.TaskID,
				AccountID: task.UserID,
				UserID:    task.UserID,
				Status:    models.StatusFailed,
				Progress:  0,
				ErrorCode: "API_UNAVAILABLE",
				Message:   safeMsg,
				Error:     safeMsg,
				Timestamp: time.Now().UTC(),
			})
		}
	}()

	// Skip immediately if the task was cancelled while sitting in the queue
	if task.Ctx != nil && task.Ctx.Err() != nil {
		log.Printf("[ocr_worker] task %s cancelled while queued before processing", task.TaskID)
		return
	}

	baseCtx := task.Ctx
	if baseCtx == nil {
		baseCtx = ctx
	}
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 60*time.Second)
	defer timeoutCancel()

	wp.cancelsMu.Lock()
	wp.activeCancels[task.TaskID] = activeTaskCancel{
		userID: task.UserID,
		cancel: func() {
			if task.Cancel != nil {
				task.Cancel()
			}
			timeoutCancel()
		},
	}
	wp.cancelsMu.Unlock()

	// 1. Transition: PROCESSING
	wp.emitEvent(models.OCREventPayload{
		Event:     "ocr_progress",
		RequestID: task.TaskID,
		TaskID:    task.TaskID,
		AccountID: task.UserID,
		UserID:    task.UserID,
		Status:    models.StatusProcessing,
		Progress:  50,
		Timestamp: time.Now().UTC(),
	})

	// 2. Perform in-memory text extraction using task-scoped context (cancellable)
	res, err := wp.engine.ExtractText(taskCtx, task.ImageBytes)
	if err != nil {
		if errors.Is(err, context.Canceled) || (taskCtx.Err() != nil && errors.Is(taskCtx.Err(), context.Canceled)) {
			log.Printf("[ocr_worker] task %s cancelled mid-processing by client disconnect", task.TaskID)
			return
		}
		log.Printf("[ocr_worker] task %s failed: %v", task.TaskID, err)
		const safeMsg = "Dịch vụ bận, vui lòng thử lại hoặc nhập thủ công."
		wp.emitEvent(models.OCREventPayload{
			Event:     "ocr_failed",
			RequestID: task.TaskID,
			TaskID:    task.TaskID,
			AccountID: task.UserID,
			UserID:    task.UserID,
			Status:    models.StatusFailed,
			Progress:  0,
			ErrorCode: "API_UNAVAILABLE",
			Message:   safeMsg,
			Error:     sanitizeWorkerError(err, safeMsg),
			Timestamp: time.Now().UTC(),
		})
		return
	}

	// 3. Transition: COMPLETED (standardize payload for 9-digit CMND / 12-digit CCCD & front/back side)
	res = StandardizeOCRResult(res)
	side := strings.ToLower(strings.TrimSpace(task.Side))
	if side == "back" || strings.EqualFold(res.Side, "back") {
		side = "back"
	} else {
		side = "front"
	}
	res.Side = side

	eventName := "ocr_completed"
	if side == "back" {
		eventName = "ocr_back_completed"
	}

	wp.emitEvent(models.OCREventPayload{
		Event:     eventName,
		RequestID: task.TaskID,
		TaskID:    task.TaskID,
		AccountID: task.UserID,
		UserID:    task.UserID,
		Side:      side,
		Status:    models.StatusCompleted,
		Progress:  100,
		Data:      res,
		Timestamp: time.Now().UTC(),
	})
	log.Printf("[ocr_worker] task %s (%s) completed successfully for user %d", task.TaskID, eventName, task.UserID)
}

func statusOrderRank(s models.OCRStatus) int {
	switch s {
	case models.StatusQueued:
		return 1
	case models.StatusProcessing:
		return 2
	case models.StatusCompleted, models.StatusFailed:
		return 3
	default:
		return 0
	}
}

func (wp *WorkerPool) emitEvent(payload models.OCREventPayload) {
	if payload.RequestID == "" {
		payload.RequestID = payload.TaskID
	}
	if payload.AccountID == 0 {
		payload.AccountID = payload.UserID
	}

	// O(1) ephemeral store write with monotonic status guard (prevents stale state overwrite)
	if payload.TaskID != "" {
		wp.tasksMu.Lock()
		if existing, exists := wp.tasks[payload.TaskID]; !exists || statusOrderRank(payload.Status) >= statusOrderRank(existing.Status) {
			wp.tasks[payload.TaskID] = payload
		}
		wp.tasksMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. Zero-PII Redis Pub/Sub: never publish raw PII fields (Data) onto shared Redis channel cccd:ocr_events.
	if wp.redisSvc != nil {
		redisSignal := payload
		redisSignal.Data = nil
		if err := wp.redisSvc.Publish(ctx, wp.channel, redisSignal); err != nil {
			log.Printf("[ocr_worker] redis publish error on %s: %v", wp.channel, err)
		}
	}

	// 2. Targeted in-memory dispatch to authenticated user's WebSocket session via Hub.
	if wp.dispatcher != nil {
		wp.dispatcher(payload)
	}
}

// GetTaskStatus returns the most recent OCREventPayload for a taskID if owned by userID.
// Enforces O(1) lazy expiration (max 3-minute TTL) and single-read auto-purge for completed PII payloads.
func (wp *WorkerPool) GetTaskStatus(taskID string, userID int64) (models.OCREventPayload, bool) {
	now := time.Now().UTC()
	wp.tasksMu.Lock()
	defer wp.tasksMu.Unlock()

	rec, ok := wp.tasks[taskID]
	if !ok {
		return models.OCREventPayload{}, false
	}
	if now.Sub(rec.Timestamp) > DefaultTaskTTL {
		delete(wp.tasks, taskID)
		return models.OCREventPayload{}, false
	}
	if userID <= 0 || rec.UserID <= 0 || rec.UserID != userID {
		return models.OCREventPayload{}, false
	}
	// Single-read ephemeral store: auto-purge completed PII payload immediately upon read
	if rec.Status == models.StatusCompleted || rec.Status == models.StatusFailed {
		delete(wp.tasks, taskID)
	}
	return rec, true
}

// -----------------------------------------------------------------------------
// OCREngine Implementations
// -----------------------------------------------------------------------------

// LocalScriptOCREngine executes a localized inference script or CLI tool via stdin (zero disk writes).
type LocalScriptOCREngine struct {
	commandPath string
	args        []string
}

// NewLocalScriptOCREngine constructs a script-based text extractor.
func NewLocalScriptOCREngine(commandPath string, args ...string) *LocalScriptOCREngine {
	return &LocalScriptOCREngine{
		commandPath: commandPath,
		args:        args,
	}
}

// ExtractText executes the localized CLI script passing imageBytes via stdin.
// Strictly enforces textual extraction only (no face detection, no disk writes).
func (e *LocalScriptOCREngine) ExtractText(ctx context.Context, imageBytes []byte) (*models.OCRResultData, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("empty image bytes")
	}

	cmd := exec.CommandContext(ctx, e.commandPath, e.args...)
	cmd.Stdin = bytes.NewReader(imageBytes)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("local ocr execution error: %v (output: %s)", err, string(out))
	}

	return parseRawOCRText(string(out)), nil
}

// MockOCREngine provides a lightweight, deterministic text extractor for tests.
type MockOCREngine struct {
	Result *models.OCRResultData
	Err    error
}

// ExtractText returns synthetic mock data or error from in-memory imageBytes.
func (m *MockOCREngine) ExtractText(_ context.Context, imageBytes []byte) (*models.OCRResultData, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("empty image bytes")
	}
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Result != nil {
		return m.Result, nil
	}
	return &models.OCRResultData{
		IDNumber: "001099000001",
		FullName: "NGUYỄN VĂN A",
		DOB:      "15/03/1999",
		Address:  "Xã Nghi Phú, TP Vinh, Nghệ An",
		HomeTown: "Nghệ An",
	}, nil
}

// regexStrictID matches strictly 12-digit CCCD or 9-digit CMND numbers.
var (
	regexStrictID        = regexp.MustCompile(`^(\d{9}|\d{12})$`)
	regexExtractIDLine   = regexp.MustCompile(`(?i)(?:số|so|no|id)?[:\s]*\b(\d{12}|\d{9})\b`)
	regexEthnicityPrefix = regexp.MustCompile(`(?i)^(dân\s*tộc|dan\s*toc|ethnicity)\s*[:.\-]?\s*`)
	regexReligionPrefix  = regexp.MustCompile(`(?i)^(tôn\s*giáo|ton\s*giao|religion)\s*[:.\-]?\s*`)
)

// StandardizeOCRResult normalizes extracted OCR fields and ensures 9/12-digit ID compliance.
// Strictly preserves text-only fields (zero biometrics).
func StandardizeOCRResult(res *models.OCRResultData) *models.OCRResultData {
	if res == nil {
		return &models.OCRResultData{}
	}
	cleanedID := strings.TrimSpace(res.IDNumber)
	if !regexStrictID.MatchString(cleanedID) {
		// Attempt to extract valid 9 or 12 digit sequence if surrounding noise exists
		if m := regexExtractIDLine.FindStringSubmatch(cleanedID); len(m) > 1 {
			cleanedID = m[1]
		} else {
			cleanedID = ""
		}
	}
	res.IDNumber = cleanedID
	res.FullName = strings.ToUpper(strings.TrimSpace(res.FullName))
	res.DOB = strings.TrimSpace(res.DOB)
	res.Sex = strings.TrimSpace(res.Sex)
	res.Gender = strings.TrimSpace(res.Gender)
	if res.Gender == "" && res.Sex != "" {
		res.Gender = res.Sex
	} else if res.Sex == "" && res.Gender != "" {
		res.Sex = res.Gender
	}
	// Strict safeguard: 9-digit CMND physically has no printed gender; never auto-infer
	// and never fabricate gender from 12-digit ID numbers when sex is unread.
	if len(cleanedID) == 9 {
		res.Gender = ""
		res.Sex = ""
	}

	res.Ethnicity = normalizeEthnicityField(res.Ethnicity)
	res.Religion = normalizeReligionField(res.Religion)
	res.Nationality = strings.TrimSpace(res.Nationality)

	res.Address = strings.TrimSpace(res.Address)
	res.AddressProvince = strings.TrimSpace(res.AddressProvince)
	res.AddressDistrict = strings.TrimSpace(res.AddressDistrict)
	res.AddressWard = strings.TrimSpace(res.AddressWard)
	res.AddressDetail = strings.TrimSpace(res.AddressDetail)

	if res.Address != "" {
		fbProvince, fbDistrict, fbWard, fbDetail := splitVietnameseAddressFallback(res.Address)
		if res.AddressProvince == "" {
			res.AddressProvince = fbProvince
		}
		if res.AddressDistrict == "" {
			res.AddressDistrict = fbDistrict
		}
		if res.AddressWard == "" {
			res.AddressWard = fbWard
		}
		if res.AddressDetail == "" {
			res.AddressDetail = fbDetail
		}
	}

	if res.Address != "" || res.AddressProvince != "" || res.AddressWard != "" {
		resolved := matcher.DefaultResolver().Resolve(
			res.Address,
			res.AddressProvince,
			res.AddressDistrict,
			res.AddressWard,
			res.AddressDetail,
		)
		res.AddressResolutionStatus = resolved.Status
		if resolved.Status == matcher.StatusRequiresConfirmation {
			res.AddressCandidates = resolved.Candidates
			if resolved.ProvinceName != "" {
				res.AddressProvince = resolved.ProvinceName
			}
			if resolved.ProvinceID > 0 {
				pID := resolved.ProvinceID
				res.ProvinceID = &pID
			}
			res.WardID = nil
		} else if resolved.Matched {
			if res.AddressProvince == "" || resolved.LegacyProvince != "" {
				if resolved.ProvinceName != "" {
					res.AddressProvince = resolved.ProvinceName
				}
			}
			if resolved.DistrictName != "" && res.AddressDistrict == "" {
				res.AddressDistrict = resolved.DistrictName
			}
			if res.AddressWard == "" || resolved.MatchSource == "legacy_to_v2" {
				if resolved.WardName != "" {
					res.AddressWard = resolved.WardName
				}
			}
			if res.AddressDetail == "" && resolved.AddressDetail != "" {
				res.AddressDetail = resolved.AddressDetail
			}
			if resolved.ProvinceID > 0 {
				pID := resolved.ProvinceID
				res.ProvinceID = &pID
			}
			if resolved.DistrictID > 0 {
				dID := resolved.DistrictID
				res.DistrictID = &dID
			}
			if resolved.WardID > 0 {
				wID := resolved.WardID
				res.WardID = &wID
			}
			res.IsLegacyAddress = resolved.IsLegacyAddress
			res.LegacyWardName = resolved.LegacyWardName
		}
	}

	res.HomeTown = strings.TrimSpace(res.HomeTown)
	res.IssueDate = strings.TrimSpace(res.IssueDate)
	res.PlaceOfIssue = normalizePlaceOfIssue(res.PlaceOfIssue)
	res.ExpiryDate = strings.TrimSpace(res.ExpiryDate)
	return res
}

func splitVietnameseAddressFallback(fullAddress string) (province, district, ward, detail string) {
	rawParts := strings.Split(fullAddress, ",")
	parts := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	n := len(parts)
	if n == 0 {
		return "", "", "", ""
	}
	if n == 1 {
		return parts[0], "", "", ""
	}
	if n == 2 {
		return parts[1], "", parts[0], ""
	}
	if n == 3 {
		return parts[2], parts[1], parts[0], ""
	}
	return parts[n-1], parts[n-2], parts[n-3], strings.Join(parts[:n-3], ", ")
}

func normalizeEthnicityField(raw string) string {
	trimmed := strings.TrimSpace(regexEthnicityPrefix.ReplaceAllString(strings.TrimSpace(raw), ""))
	if trimmed == "" || strings.EqualFold(trimmed, "null") || strings.EqualFold(trimmed, "n/a") {
		return ""
	}
	words := strings.Fields(trimmed)
	for i, w := range words {
		runes := []rune(w)
		if len(runes) > 0 {
			words[i] = strings.ToUpper(string(runes[0])) + strings.ToLower(string(runes[1:]))
		}
	}
	return strings.Join(words, " ")
}

func normalizeReligionField(raw string) string {
	trimmed := strings.TrimSpace(regexReligionPrefix.ReplaceAllString(strings.TrimSpace(raw), ""))
	if trimmed == "" || strings.EqualFold(trimmed, "null") || strings.EqualFold(trimmed, "n/a") {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if lower == "không" || lower == "khong" || lower == "none" || lower == "không có" || lower == "khong co" {
		return "Không"
	}
	return trimmed
}

func normalizePlaceOfIssue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	if strings.Contains(upper, "QUẢN LÝ HÀNH CHÍNH") || strings.Contains(upper, "QLHC") {
		return "Cục Cảnh sát QLHC về TTXH"
	}
	if upper == "BỘ CÔNG AN" || strings.Contains(upper, "BỘ CÔNG AN") {
		return "Bộ Công an"
	}
	return trimmed
}

// parseRawOCRText extracts CMND (9-digit) / CCCD (12-digit) fields from plain text via regex rules.
func parseRawOCRText(text string) *models.OCRResultData {
	res := &models.OCRResultData{}

	// Match strictly 12-digit CCCD or 9-digit CMND number
	if match := regexExtractIDLine.FindStringSubmatch(text); len(match) > 1 {
		res.IDNumber = match[1]
	}

	// Match Full Name
	nameRegex := regexp.MustCompile(`(?i)(?:họ và tên|họ tên|full name)[:\s]*([A-ZÀ-Ỹ\s]+)`)
	if match := nameRegex.FindStringSubmatch(text); len(match) > 1 {
		res.FullName = strings.TrimSpace(match[1])
	}

	// Match Date of Birth (dd/mm/yyyy)
	dobRegex := regexp.MustCompile(`(?i)(?:sinh ngày|ngày sinh|date of birth|dob)[:\s]*([0-9]{1,2}[/-][0-9]{1,2}[/-][0-9]{4})`)
	if match := dobRegex.FindStringSubmatch(text); len(match) > 1 {
		res.DOB = strings.ReplaceAll(match[1], "-", "/")
	}

	// Match Address / Nơi thường trú / Nguyên quán
	addrRegex := regexp.MustCompile(`(?i)(?:nơi thường trú|thường trú|nơi đkhk thường trú|cư trú|place of residence)[:\s]*([^\n]+)`)
	if match := addrRegex.FindStringSubmatch(text); len(match) > 1 {
		res.Address = strings.TrimSpace(match[1])
	}

	return StandardizeOCRResult(res)
}
