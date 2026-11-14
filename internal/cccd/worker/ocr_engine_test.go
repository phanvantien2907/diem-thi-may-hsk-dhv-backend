package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/cccd/dto/response"
	"github.com/hskvinh/backend/internal/cccd/enum"
	"github.com/hskvinh/backend/internal/cccd/models"
	"github.com/hskvinh/backend/internal/cccd/worker"
)

type mockRedisPublisher struct {
	mu       sync.Mutex
	messages []models.OCREventPayload
}

func (m *mockRedisPublisher) Publish(_ context.Context, _ string, message any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if payload, ok := message.(models.OCREventPayload); ok {
		m.messages = append(m.messages, payload)
	}
	return nil
}

func (m *mockRedisPublisher) getMessages() []models.OCREventPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]models.OCREventPayload, len(m.messages))
	copy(cp, m.messages)
	return cp
}

func TestWorkerPool_StateTransitionsAndZeroPIIRedis(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Result: &models.OCRResultData{
			IDNumber: "001099000001",
			FullName: "NGUYỄN VĂN A",
			DOB:      "15/03/1999",
			Address:  "Xã Nghi Phú, TP Vinh, Nghệ An",
		},
	}

	var directEvents []models.OCREventPayload
	var mu sync.Mutex

	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, func(p models.OCREventPayload) {
		mu.Lock()
		directEvents = append(directEvents, p)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	imageBuf := []byte("in-memory-cccd-jpeg-bytes")
	task := models.OCRTask{
		TaskID:     "task-uuid-1",
		UserID:     202,
		ImageBytes: imageBuf,
		CreatedAt:  time.Now().UTC(),
	}

	if err := wp.Enqueue(task); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Wait for worker to complete (allow up to 5s for cold-start DefaultResolver snapshot load under -race)
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		msgs := mockPub.getMessages()
		for _, m := range msgs {
			if m.TaskID == "task-uuid-1" && m.Status == models.StatusCompleted {
				completed = true
				break
			}
		}
		if completed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !completed {
		t.Fatalf("worker did not emit COMPLETED event in time; messages: %+v", mockPub.getMessages())
	}

	// 1. Verify State Transitions: QUEUED (10) -> PROCESSING (50) -> COMPLETED (100)
	msgs := mockPub.getMessages()
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(msgs))
	}

	if msgs[0].Status != models.StatusQueued || msgs[0].Progress != 10 {
		t.Errorf("expected 1st event QUEUED(10), got %s(%d)", msgs[0].Status, msgs[0].Progress)
	}
	if msgs[1].Status != models.StatusProcessing || msgs[1].Progress != 50 {
		t.Errorf("expected 2nd event PROCESSING(50), got %s(%d)", msgs[1].Status, msgs[1].Progress)
	}
	if msgs[2].Status != models.StatusCompleted || msgs[2].Progress != 100 {
		t.Errorf("expected 3rd event COMPLETED(100), got %s(%d)", msgs[2].Status, msgs[2].Progress)
	}

	// 2. Verify Zero-PII Redis Pub/Sub (Decree 13/2023): Redis payload MUST NOT contain raw PII Data
	if msgs[2].Data != nil {
		t.Errorf("expected Redis Pub/Sub payload Data to be nil (Zero-PII policy), got %+v", msgs[2].Data)
	}

	// 3. Verify targeted WebSocket dispatcher received the full PII data
	mu.Lock()
	var wsCompleted *models.OCREventPayload
	for i := range directEvents {
		if directEvents[i].TaskID == "task-uuid-1" && directEvents[i].Status == models.StatusCompleted {
			cp := directEvents[i]
			wsCompleted = &cp
			break
		}
	}
	mu.Unlock()
	if wsCompleted == nil || wsCompleted.Data == nil || wsCompleted.Data.IDNumber != "001099000001" || wsCompleted.Data.FullName != "NGUYỄN VĂN A" {
		t.Errorf("expected targeted WebSocket dispatcher to receive full extracted data, got %+v", wsCompleted)
	}

	// 4. Verify single-read ephemeral store auto-purges completed PII upon read
	ephemeralRec, ok := wp.GetTaskStatus("task-uuid-1", 202)
	if !ok || ephemeralRec.Data == nil || ephemeralRec.Data.IDNumber != "001099000001" {
		t.Fatalf("expected first GetTaskStatus to return completed payload, got ok=%v rec=%+v", ok, ephemeralRec)
	}
	if _, okSecond := wp.GetTaskStatus("task-uuid-1", 202); okSecond {
		t.Errorf("expected completed PII payload to be auto-purged after single read")
	}
}

func TestWorkerPool_FailureState(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Err: errors.New("cannot read text from blurry document"),
	}

	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	_ = wp.Enqueue(models.OCRTask{
		TaskID:     "task-fail-1",
		UserID:     202,
		ImageBytes: []byte("corrupted-in-memory-image"),
	})

	deadline := time.Now().Add(2 * time.Second)
	failed := false
	for time.Now().Before(deadline) {
		for _, m := range mockPub.getMessages() {
			if m.TaskID == "task-fail-1" && m.Status == models.StatusFailed {
				failed = true
				if m.Error != "cannot read text from blurry document" {
					t.Errorf("unexpected error message: %s", m.Error)
				}
				break
			}
		}
		if failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !failed {
		t.Fatalf("worker did not emit FAILED event; messages: %+v", mockPub.getMessages())
	}
}

func TestStandardizeOCRResult_9And12DigitSupport(t *testing.T) {
	// 1. Valid 9-digit legacy CMND
	cmndRes := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: " 123456789 ",
		FullName: " trần thị b ",
		DOB:      " 20/10/1995 ",
	})
	if cmndRes.IDNumber != "123456789" {
		t.Errorf("expected 9-digit CMND '123456789', got %q", cmndRes.IDNumber)
	}
	if cmndRes.FullName != "TRẦN THỊ B" {
		t.Errorf("expected uppercase FullName 'TRẦN THỊ B', got %q", cmndRes.FullName)
	}

	// 2. Valid 12-digit modern CCCD
	cccdRes := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: "001099000001",
		FullName: "Lê Văn C",
	})
	if cccdRes.IDNumber != "001099000001" {
		t.Errorf("expected 12-digit CCCD '001099000001', got %q", cccdRes.IDNumber)
	}

	// 3. Invalid 10-digit noise is stripped
	invalidRes := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: "0123456789",
	})
	if invalidRes.IDNumber != "" {
		t.Errorf("expected 10-digit invalid ID to be stripped to empty, got %q", invalidRes.IDNumber)
	}
}

func TestStandardizeOCRResult_AddressSplitAndGender(t *testing.T) {
	res := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber:     "040203000001",
		FullName:     "Nguyễn Văn A",
		DOB:          "29/07/2003",
		Sex:          "Nam",
		Address:      "Số 12 Lê Lợi, P. Hà Huy Tập, TP. Vinh, Nghệ An",
		PlaceOfIssue: "CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
	})
	if res.Gender != "Nam" || res.Sex != "Nam" {
		t.Fatalf("expected Gender='Nam' synced from Sex='Nam', got Gender=%q Sex=%q", res.Gender, res.Sex)
	}
	if res.AddressProvince != "Nghệ An" {
		t.Fatalf("expected AddressProvince='Nghệ An', got %q", res.AddressProvince)
	}
	if res.AddressDistrict != "TP. Vinh" {
		t.Fatalf("expected AddressDistrict='TP. Vinh', got %q", res.AddressDistrict)
	}
	if res.AddressWard != "P. Hà Huy Tập" {
		t.Fatalf("expected AddressWard='P. Hà Huy Tập', got %q", res.AddressWard)
	}
	if res.AddressDetail != "Số 12 Lê Lợi" {
		t.Fatalf("expected AddressDetail='Số 12 Lê Lợi', got %q", res.AddressDetail)
	}
	if res.PlaceOfIssue != "Cục Cảnh sát QLHC về TTXH" {
		t.Fatalf("expected normalized PlaceOfIssue='Cục Cảnh sát QLHC về TTXH', got %q", res.PlaceOfIssue)
	}
}

func TestWorkerPool_BackSideEmitsOCRBackCompleted(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Result: &models.OCRResultData{
			Side:         "back",
			IssueDate:    "10/05/2021",
			PlaceOfIssue: "CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
		},
	}

	var directEvents []models.OCREventPayload
	var mu sync.Mutex
	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, func(p models.OCREventPayload) {
		mu.Lock()
		directEvents = append(directEvents, p)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	if err := wp.Enqueue(models.OCRTask{
		TaskID:     "task-back-1",
		UserID:     303,
		Side:       "back",
		ImageBytes: []byte("fake-back-image"),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enqueue back task failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var completedMsg *models.OCREventPayload
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, m := range directEvents {
			if m.TaskID == "task-back-1" && m.Status == models.StatusCompleted {
				cp := m
				completedMsg = &cp
				break
			}
		}
		mu.Unlock()
		if completedMsg != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if completedMsg == nil {
		t.Fatalf("timed out waiting for back-side OCR task completion")
	}
	if completedMsg.Event != "ocr_back_completed" || completedMsg.Side != "back" {
		t.Fatalf("expected event='ocr_back_completed' and side='back', got event=%q side=%q", completedMsg.Event, completedMsg.Side)
	}
	if completedMsg.Data == nil || completedMsg.Data.IssueDate != "10/05/2021" || completedMsg.Data.PlaceOfIssue != "Cục Cảnh sát QLHC về TTXH" {
		t.Fatalf("unexpected back-side data: %+v", completedMsg.Data)
	}
}

func TestStandardizeOCRResult_9DigitCMNDAnd12DigitCCCDJSONKeys(t *testing.T) {
	// 9-digit CMND: missing gender, prefixes on ethnicity & religion
	cmnd := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber:  "123456789",
		FullName:  "nguyễn văn a",
		DOB:       "24/09/1987",
		Gender:    "",
		Ethnicity: "Dân tộc: Kinh",
		Religion:  "Tôn giáo: Không",
		Address:   "P. Hà Huy Tập, TP. Vinh, Nghệ An",
	})
	if cmnd.Gender != "" {
		t.Fatalf("expected empty Gender for 9-digit CMND without gender on card, got %q", cmnd.Gender)
	}
	if cmnd.Ethnicity != "Kinh" || cmnd.Religion != "Không" {
		t.Fatalf("expected Ethnicity='Kinh' and Religion='Không', got Ethnicity=%q Religion=%q", cmnd.Ethnicity, cmnd.Religion)
	}

	rawBytes, err := json.Marshal(cmnd)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(rawBytes, &asMap); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	requiredKeys := []string{
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
	}
	for _, key := range requiredKeys {
		if _, exists := asMap[key]; !exists {
			t.Fatalf("expected marshaled JSON to always contain key %q, got JSON: %s", key, string(rawBytes))
		}
	}

	// Verify 12-digit ID with empty gender does NOT silently fabricate gender from digit 3 (H-04)
	cccd12UnreadSex := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: "001099000001",
		FullName: "NGUYEN VAN A",
		Gender:   "",
		Sex:      "",
	})
	if cccd12UnreadSex.Gender != "" || cccd12UnreadSex.Sex != "" {
		t.Fatalf("expected 12-digit ID with unread sex to keep Gender='' and Sex='' (no silent guessing), got Gender=%q Sex=%q", cccd12UnreadSex.Gender, cccd12UnreadSex.Sex)
	}
}

type blockingCancelEngine struct {
	started chan struct{}
}

func (b *blockingCancelEngine) ExtractText(ctx context.Context, _ []byte) (*models.OCRResultData, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestWorkerPool_ContextCancellationAbortsProcessing(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	engine := &blockingCancelEngine{started: make(chan struct{})}

	wp := worker.NewWorkerPool(engine, 1, 10, mockPub, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	taskCtx, taskCancel := context.WithCancel(context.Background())
	task := models.OCRTask{
		TaskID:     "task-cancel-1",
		UserID:     505,
		ImageBytes: []byte("cancel-test-bytes"),
		Ctx:        taskCtx,
		Cancel:     taskCancel,
		CreatedAt:  time.Now().UTC(),
	}

	if err := wp.Enqueue(task); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Wait for engine to start processing
	select {
	case <-engine.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for engine to start")
	}

	// Simulate client disconnection / tab closure by cancelling user's tasks
	wp.CancelUserTasks(505)

	// Wait for processing cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify no COMPLETED event was emitted
	for _, m := range mockPub.getMessages() {
		if m.TaskID == "task-cancel-1" && m.Status == models.StatusCompleted {
			t.Fatalf("unexpected COMPLETED event after cancellation: %+v", m)
		}
	}
}

type slowQueuedPublishMock struct {
	mu       sync.Mutex
	messages []models.OCREventPayload
}

func (s *slowQueuedPublishMock) Publish(_ context.Context, _ string, message any) error {
	payload, ok := message.(models.OCREventPayload)
	if ok && payload.Status == models.StatusQueued {
		// Simulate slight scheduling delay while Enqueue publishes QUEUED
		time.Sleep(15 * time.Millisecond)
	}
	s.mu.Lock()
	if ok {
		s.messages = append(s.messages, payload)
	}
	s.mu.Unlock()
	return nil
}

// H-01: Verify Enqueue never allows PROCESSING/COMPLETED to be emitted before QUEUED
// or overwritten by a delayed QUEUED event.
func TestWorkerPool_H01_EnqueueStrictEventOrdering(t *testing.T) {
	pub := &slowQueuedPublishMock{}
	engine := &worker.MockOCREngine{
		Result: &models.OCRResultData{
			IDNumber: "001099000001",
			FullName: "NGUYỄN VĂN A",
			DOB:      "15/03/1999",
		},
	}

	var wsEvents []models.OCRStatus
	var mu sync.Mutex
	wp := worker.NewWorkerPool(engine, 2, 16, pub, func(p models.OCREventPayload) {
		mu.Lock()
		wsEvents = append(wsEvents, p.Status)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	if err := wp.Enqueue(models.OCRTask{
		TaskID:     "task-order-h01",
		UserID:     101,
		ImageBytes: []byte("valid-image-bytes"),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(wsEvents)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	got := append([]models.OCRStatus(nil), wsEvents...)
	mu.Unlock()

	if len(got) != 3 || got[0] != models.StatusQueued || got[1] != models.StatusProcessing || got[2] != models.StatusCompleted {
		t.Fatalf("expected strict event order [QUEUED PROCESSING COMPLETED], got %v", got)
	}
}

// H-03b: Verify GetTaskStatus rejects unauthenticated/zero userID <= 0.
func TestWorkerPool_H03_GetTaskStatusRejectsNonPositiveUserID(t *testing.T) {
	wp := worker.NewWorkerPool(&worker.MockOCREngine{}, 1, 10, nil, nil)
	if err := wp.Enqueue(models.OCRTask{
		TaskID:     "task-auth-h03",
		UserID:     202,
		ImageBytes: []byte("image-bytes"),
		CreatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if _, ok := wp.GetTaskStatus("task-auth-h03", 0); ok {
		t.Fatalf("expected GetTaskStatus with userID=0 to be rejected, but it succeeded")
	}
	if _, ok := wp.GetTaskStatus("task-auth-h03", -1); ok {
		t.Fatalf("expected GetTaskStatus with userID=-1 to be rejected, but it succeeded")
	}
}

type fakePipelineProcessor struct {
	resp *response.IdCardResponseDTO
}

func (f *fakePipelineProcessor) Process(_ context.Context, _ []byte, _ string) *response.IdCardResponseDTO {
	return f.resp
}

// H-04b: Verify PipelineOCREngine does not pass through fields marked StatusInvalid (e.g. year-only DOB "1999").
func TestPipelineOCREngine_H04_DoesNotPassInvalidStatusFields(t *testing.T) {
	idVal := "001099000001"
	nameVal := "NGUYỄN VĂN A"
	invalidDOB := "1999"
	invalidSex := "Unknown"

	proc := &fakePipelineProcessor{
		resp: &response.IdCardResponseDTO{
			ErrorCode: 0,
			Data: []response.IdCardDataDTO{
				{
					Type:     enum.CardTypeNew,
					TypeNew:  enum.CardDetailCCCD12Front,
					Decision: enum.DecisionReview,
					ID:       response.FieldValueDTO{Value: &idVal, Status: enum.StatusOK},
					Name:     response.FieldValueDTO{Value: &nameVal, Status: enum.StatusOK},
					DOB:      response.FieldValueDTO{Value: &invalidDOB, Status: enum.StatusInvalid},
					Sex:      response.FieldValueDTO{Value: &invalidSex, Status: enum.StatusInvalid},
				},
			},
		},
	}

	engine := worker.NewPipelineOCREngine(proc)
	res, err := engine.ExtractText(context.Background(), []byte("test-image"))
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if res.DOB != "" {
		t.Fatalf("expected invalid-status DOB '1999' to be omitted (empty), got %q", res.DOB)
	}
	if res.Gender != "" || res.Sex != "" {
		t.Fatalf("expected invalid-status Sex to be omitted (empty), got Gender=%q Sex=%q", res.Gender, res.Sex)
	}
}

// H-05b: Verify ocr_failed event does not leak secret API keys or internal URLs in Error field.
func TestWorkerPool_H05_RedactsSensitiveErrorInOCRFailed(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Err: errors.New(`Post "https://generativelanguage.googleapis.com/v1beta/models/gemini-3-flash-preview:generateContent?key=AIzaSySuperSecretKey999": dial tcp: i/o timeout`),
	}

	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	_ = wp.Enqueue(models.OCRTask{
		TaskID:     "task-secret-err",
		UserID:     202,
		ImageBytes: []byte("img"),
	})

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range mockPub.getMessages() {
			if m.TaskID == "task-secret-err" && m.Status == models.StatusFailed {
				if m.Error != m.Message {
					t.Fatalf("expected sanitized Error matching user-safe Message %q, but leaked raw error: %q", m.Message, m.Error)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for FAILED event")
}

// H-06a & H-06d: Verify CancelUserTasks(0) does not cancel other users' tasks,
// and CancelUserTasks(userID) cancels queued tasks before ExtractText is invoked.
func TestWorkerPool_H06_CancelUserTasksBlastRadiusAndQueuedTasks(t *testing.T) {
	engine := &blockingCancelEngine{started: make(chan struct{})}
	var user2Calls atomic.Int32
	wrappedEngine := &countingBlockingEngine{
		blocking:   engine,
		user2Calls: &user2Calls,
	}

	wp := worker.NewWorkerPool(wrappedEngine, 1, 10, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	// Enqueue User 101's task (occupies the single worker)
	task1Ctx, task1Cancel := context.WithCancel(context.Background())
	defer task1Cancel()
	if err := wp.Enqueue(models.OCRTask{
		TaskID:     "task-u101",
		UserID:     101,
		ImageBytes: []byte("u101"),
		Ctx:        task1Ctx,
		Cancel:     task1Cancel,
	}); err != nil {
		t.Fatalf("enqueue u101 failed: %v", err)
	}

	select {
	case <-engine.started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for u101 task to start")
	}

	// Enqueue User 202's task while worker is busy (sits in queue)
	if err := wp.Enqueue(models.OCRTask{
		TaskID:     "task-u202-queued",
		UserID:     202,
		ImageBytes: []byte("u202"),
	}); err != nil {
		t.Fatalf("enqueue u202 failed: %v", err)
	}

	// 1. Calling CancelUserTasks(0) or CancelUserTasks(-1) MUST NOT cancel User 101's active task!
	wp.CancelUserTasks(0)
	wp.CancelUserTasks(-1)
	if task1Ctx.Err() != nil {
		t.Fatalf("CancelUserTasks(0/-1) wrongly cancelled User 101's active task: %v", task1Ctx.Err())
	}

	// 2. Cancel User 202 while their task is still queued, then release User 101
	wp.CancelUserTasks(202)
	wp.CancelUserTasks(101)

	time.Sleep(80 * time.Millisecond)
	if got := user2Calls.Load(); got != 0 {
		t.Fatalf("expected queued task for disconnected User 202 to be skipped before ExtractText, but ExtractText was called %d times", got)
	}
}

type countingBlockingEngine struct {
	blocking   *blockingCancelEngine
	user2Calls *atomic.Int32
}

func (c *countingBlockingEngine) ExtractText(ctx context.Context, img []byte) (*models.OCRResultData, error) {
	if string(img) == "u202" {
		c.user2Calls.Add(1)
		return &models.OCRResultData{IDNumber: "001099000001"}, nil
	}
	return c.blocking.ExtractText(ctx, img)
}

type panicOnFirstCallEngine struct {
	calls atomic.Int32
}

func (p *panicOnFirstCallEngine) ExtractText(_ context.Context, _ []byte) (*models.OCRResultData, error) {
	if p.calls.Add(1) == 1 {
		panic("simulated nil pointer dereference in OCR engine")
	}
	return &models.OCRResultData{
		IDNumber: "001099000001",
		FullName: "NGUYỄN VĂN A",
	}, nil
}

// H-08: Verify a panic inside OCREngine.ExtractText is recovered, emits FAILED, and keeps the worker alive.
func TestWorkerPool_H08_RecoversFromEnginePanic(t *testing.T) {
	mockPub := &mockRedisPublisher{}
	engine := &panicOnFirstCallEngine{}
	wp := worker.NewWorkerPool(engine, 1, 10, mockPub, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	_ = wp.Enqueue(models.OCRTask{
		TaskID:     "task-panic-1",
		UserID:     301,
		ImageBytes: []byte("img-1"),
	})
	_ = wp.Enqueue(models.OCRTask{
		TaskID:     "task-after-panic-2",
		UserID:     301,
		ImageBytes: []byte("img-2"),
	})

	deadline := time.Now().Add(5 * time.Second)
	sawFailed1 := false
	sawCompleted2 := false
	for time.Now().Before(deadline) {
		for _, m := range mockPub.getMessages() {
			if m.TaskID == "task-panic-1" && m.Status == models.StatusFailed {
				sawFailed1 = true
			}
			if m.TaskID == "task-after-panic-2" && m.Status == models.StatusCompleted {
				sawCompleted2 = true
			}
		}
		if sawFailed1 && sawCompleted2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected worker to recover from panic (sawFailed1=%v, sawCompleted2=%v)", sawFailed1, sawCompleted2)
}
