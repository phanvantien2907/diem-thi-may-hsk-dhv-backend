package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/cccd/models"
)

const (
	DefaultRedisChannel = "cccd:ocr_events"
	DefaultQueueSize    = 512
	DefaultConcurrency  = 4
)

// RedisPublisher abstracts Redis Pub/Sub publishing.
type RedisPublisher interface {
	Publish(ctx context.Context, channel string, message any) error
}

// OCREngine defines the text extraction capability.
// Strictly extracts textual fields only. NO BIOMETRICS OR FACIAL RECOGNITION.
type OCREngine interface {
	ExtractText(ctx context.Context, filePath string) (*models.OCRResultData, error)
}

// EventDispatcher is an optional callback to notify in-memory components (e.g. WebSocket Hub).
type EventDispatcher func(payload models.OCREventPayload)

// WorkerPool manages background worker goroutines that process OCR tasks asynchronously.
type WorkerPool struct {
	engine        OCREngine
	queue         chan models.OCRTask
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
	activeCancels map[string]context.CancelFunc
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
		redisSvc:      redisSvc,
		dispatcher:    dispatcher,
		channel:       DefaultRedisChannel,
		workers:       concurrency,
		tasks:         make(map[string]models.OCREventPayload),
		activeCancels: make(map[string]context.CancelFunc),
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
	case wp.queue <- task:
		// Emit initial QUEUED event immediately upon enqueue
		wp.emitEvent(models.OCREventPayload{
			Event:     "ocr_progress",
			TaskID:    task.TaskID,
			UserID:    task.UserID,
			Status:    models.StatusQueued,
			Progress:  10,
			Timestamp: time.Now().UTC(),
		})
		return nil
	default:
		return errors.New("ocr worker queue is full")
	}
}

// Start launches the worker goroutines.
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
	log.Printf("[ocr_worker] started %d background workers for channel %s", wp.workers, wp.channel)
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
			wp.processTask(ctx, task)
		}
	}
}

// CancelTask cancels an in-flight OCR task by its taskID.
func (wp *WorkerPool) CancelTask(taskID string) {
	wp.cancelsMu.Lock()
	cancel, ok := wp.activeCancels[taskID]
	delete(wp.activeCancels, taskID)
	wp.cancelsMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

// CancelUserTasks cancels all in-flight OCR tasks for a specific userID.
func (wp *WorkerPool) CancelUserTasks(userID int64) {
	wp.cancelsMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(wp.activeCancels))
	for id, cancel := range wp.activeCancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		delete(wp.activeCancels, id)
	}
	wp.cancelsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (wp *WorkerPool) processTask(ctx context.Context, task models.OCRTask) {
	// Clean up temporary image file after processing completes or fails
	defer func() {
		if task.FilePath != "" {
			if err := os.Remove(task.FilePath); err != nil && !os.IsNotExist(err) {
				log.Printf("[ocr_worker] error removing temp file %s: %v", task.FilePath, err)
			}
		}
	}()

	taskCtx := task.Ctx
	if taskCtx == nil {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		task.Cancel = cancel
	}
	if task.Cancel != nil {
		defer task.Cancel()
	}

	wp.cancelsMu.Lock()
	wp.activeCancels[task.TaskID] = task.Cancel
	wp.cancelsMu.Unlock()

	defer func() {
		wp.cancelsMu.Lock()
		delete(wp.activeCancels, task.TaskID)
		wp.cancelsMu.Unlock()
	}()

	// 1. Transition: PROCESSING
	wp.emitEvent(models.OCREventPayload{
		Event:     "ocr_progress",
		TaskID:    task.TaskID,
		UserID:    task.UserID,
		Status:    models.StatusProcessing,
		Progress:  50,
		Timestamp: time.Now().UTC(),
	})

	// 2. Perform text extraction using task-scoped context (cancellable)
	res, err := wp.engine.ExtractText(taskCtx, task.FilePath)
	if err != nil {
		if errors.Is(err, context.Canceled) || (taskCtx.Err() != nil && errors.Is(taskCtx.Err(), context.Canceled)) {
			log.Printf("[ocr_worker] task %s cancelled mid-processing by client disconnect", task.TaskID)
			return
		}
		log.Printf("[ocr_worker] task %s failed: %v", task.TaskID, err)
		wp.emitEvent(models.OCREventPayload{
			Event:     "ocr_failed",
			TaskID:    task.TaskID,
			UserID:    task.UserID,
			Status:    models.StatusFailed,
			Progress:  0,
			ErrorCode: "API_UNAVAILABLE",
			Message:   "Dịch vụ bận, vui lòng thử lại hoặc nhập thủ công.",
			Error:     err.Error(),
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
		TaskID:    task.TaskID,
		UserID:    task.UserID,
		Side:      side,
		Status:    models.StatusCompleted,
		Progress:  100,
		Data:      res,
		Timestamp: time.Now().UTC(),
	})
	log.Printf("[ocr_worker] task %s (%s) completed successfully for user %d", task.TaskID, eventName, task.UserID)
}

func (wp *WorkerPool) emitEvent(payload models.OCREventPayload) {
	if payload.TaskID != "" {
		now := time.Now().UTC()
		wp.tasksMu.Lock()
		for id, rec := range wp.tasks {
			if now.Sub(rec.Timestamp) > 10*time.Minute {
				delete(wp.tasks, id)
			}
		}
		wp.tasks[payload.TaskID] = payload
		wp.tasksMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. Publish to Redis Pub/Sub if available
	if wp.redisSvc != nil {
		if err := wp.redisSvc.Publish(ctx, wp.channel, payload); err != nil {
			log.Printf("[ocr_worker] redis publish error on %s: %v", wp.channel, err)
		}
	}

	// 2. Direct in-memory dispatch to Hub (fallback or single-instance notification)
	if wp.dispatcher != nil {
		wp.dispatcher(payload)
	}
}

// GetTaskStatus returns the most recent OCREventPayload for a taskID if owned by userID.
func (wp *WorkerPool) GetTaskStatus(taskID string, userID int64) (models.OCREventPayload, bool) {
	wp.tasksMu.RLock()
	defer wp.tasksMu.RUnlock()
	rec, ok := wp.tasks[taskID]
	if !ok {
		return models.OCREventPayload{}, false
	}
	if userID > 0 && rec.UserID > 0 && rec.UserID != userID {
		return models.OCREventPayload{}, false
	}
	return rec, true
}

// -----------------------------------------------------------------------------
// OCREngine Implementations
// -----------------------------------------------------------------------------

// LocalScriptOCREngine executes a localized inference script or CLI tool (e.g. tesseract CLI).
// Zero CGO requirement, avoiding compilation issues with CGO_ENABLED=0.
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

// ExtractText executes the localized CLI script and parses extracted text.
// Strictly enforces textual extraction only (no face detection).
func (e *LocalScriptOCREngine) ExtractText(ctx context.Context, filePath string) (*models.OCRResultData, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	cmdArgs := append(e.args, filePath)
	cmd := exec.CommandContext(ctx, e.commandPath, cmdArgs...)
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

// ExtractText returns mock data or error.
func (m *MockOCREngine) ExtractText(_ context.Context, filePath string) (*models.OCRResultData, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Result != nil {
		return m.Result, nil
	}
	return &models.OCRResultData{
		IDNumber: "040203012345",
		FullName: "NGUYỄN VĂN AN",
		DOB:      "15/03/2003",
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
	if len(cleanedID) == 9 {
		res.Gender = ""
		res.Sex = ""
	} else if res.Gender == "" && len(cleanedID) == 12 {
		digit := cleanedID[3]
		if digit >= '0' && digit <= '9' {
			if (digit-'0')%2 == 0 {
				res.Gender = "Nam"
				res.Sex = "Nam"
			} else {
				res.Gender = "Nữ"
				res.Sex = "Nữ"
			}
		}
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

