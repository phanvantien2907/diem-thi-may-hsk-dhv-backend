package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestWorkerPool_StateTransitionsAndFileCleanup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ocr_worker_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "sample_cccd.jpg")
	if err := os.WriteFile(filePath, []byte("fake-image-data"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Result: &models.OCRResultData{
			IDNumber: "040203012345",
			FullName: "NGUYỄN VĂN AN",
			DOB:      "15/03/2003",
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

	task := models.OCRTask{
		TaskID:    "task-uuid-1",
		UserID:    202,
		FilePath:  filePath,
		CreatedAt: time.Now().UTC(),
	}

	if err := wp.Enqueue(task); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Wait for worker to complete
	deadline := time.Now().Add(2 * time.Second)
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

	// 2. Verify Output Data (Textual only, NO biometrics)
	data := msgs[2].Data
	if data == nil || data.IDNumber != "040203012345" || data.FullName != "NGUYỄN VĂN AN" {
		t.Errorf("unexpected extracted text data: %+v", data)
	}

	// 3. Verify Temp File was Cleaned Up
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected temp file %s to be deleted after processing, but it still exists", filePath)
	}
}

func TestWorkerPool_FailureState(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "ocr_fail_test_*")
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "corrupted.jpg")
	_ = os.WriteFile(filePath, []byte("corrupted"), 0600)

	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Err: errors.New("cannot read text from blurry document"),
	}

	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	_ = wp.Enqueue(models.OCRTask{
		TaskID:   "task-fail-1",
		UserID:   202,
		FilePath: filePath,
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

	// Verify temp file is cleaned up even on failure
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected temp file %s to be deleted on failure, but it still exists", filePath)
	}
}

func TestStandardizeOCRResult_9And12DigitSupport(t *testing.T) {
	// 1. Valid 9-digit legacy CMND
	cmndRes := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: " 187654321 ",
		FullName: " trần thị b ",
		DOB:      " 20/10/1995 ",
	})
	if cmndRes.IDNumber != "187654321" {
		t.Errorf("expected 9-digit CMND '187654321', got %q", cmndRes.IDNumber)
	}
	if cmndRes.FullName != "TRẦN THỊ B" {
		t.Errorf("expected uppercase FullName 'TRẦN THỊ B', got %q", cmndRes.FullName)
	}

	// 2. Valid 12-digit modern CCCD
	cccdRes := worker.StandardizeOCRResult(&models.OCRResultData{
		IDNumber: "038099012345",
		FullName: "Lê Văn C",
	})
	if cccdRes.IDNumber != "038099012345" {
		t.Errorf("expected 12-digit CCCD '038099012345', got %q", cccdRes.IDNumber)
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
		IDNumber:     "040203012345",
		FullName:     "Phan Văn Tiến",
		DOB:          "29/07/2003",
		Address:      "Số 12 Lê Lợi, P. Hà Huy Tập, TP. Vinh, Nghệ An",
		PlaceOfIssue: "CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
	})
	if res.Gender != "Nam" || res.Sex != "Nam" {
		t.Fatalf("expected Gender='Nam' inferred from 12-digit CCCD 4th digit '2', got Gender=%q Sex=%q", res.Gender, res.Sex)
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
	tempDir, err := os.MkdirTemp("", "ocr_back_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "cccd_back.jpg")
	if err := os.WriteFile(filePath, []byte("fake-back-image"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mockPub := &mockRedisPublisher{}
	mockEngine := &worker.MockOCREngine{
		Result: &models.OCRResultData{
			Side:         "back",
			IssueDate:    "10/05/2021",
			PlaceOfIssue: "CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
		},
	}

	wp := worker.NewWorkerPool(mockEngine, 1, 10, mockPub, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wp.Start(ctx)

	if err := wp.Enqueue(models.OCRTask{
		TaskID:    "task-back-1",
		UserID:    303,
		Side:      "back",
		FilePath:  filePath,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enqueue back task failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var completedMsg *models.OCREventPayload
	for time.Now().Before(deadline) {
		for _, m := range mockPub.getMessages() {
			if m.TaskID == "task-back-1" && m.Status == models.StatusCompleted {
				cp := m
				completedMsg = &cp
				break
			}
		}
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
		IDNumber:  "188020231",
		FullName:  "hoàng minh trung",
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
}


