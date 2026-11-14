package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/ws"
)

func TestHub_OCRCompleted_DeliveredToSpecificUser(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()

	targetUserID := int64(999)
	otherUserID := int64(888)

	// Create test clients for target user and other user
	clientTarget := ws.NewClient(hub, nil, targetUserID, "candidate")
	clientOther := ws.NewClient(hub, nil, otherUserID, "candidate")

	time.Sleep(20 * time.Millisecond)

	ocrPayload := map[string]interface{}{
		"event":   "ocr_completed",
		"task_id": "task-uuid-abc-123",
		"status":  "COMPLETED",
		"data": map[string]interface{}{
			"id_number":  "040203000001",
			"full_name":  "NGUYỄN VĂN A",
			"dob":        "15/03/2003",
			"address":    "Xã Nghi Phú, TP Vinh, Nghệ An",
			"home_town":  "Nghệ An",
			"issue_date": "15/03/2021",
		},
	}

	// Dispatch targeted OCR completed event
	hub.SendToUser(targetUserID, ws.EventOCRCompleted, ocrPayload)

	// Target client must receive the event
	select {
	case msg := <-clientTarget.SendChan():
		var env struct {
			Event   string `json:"event"`
			Type    string `json:"type"`
			Payload struct {
				Event   string `json:"event"`
				TaskID  string `json:"task_id"`
				Status  string `json:"status"`
				Data    struct {
					IDNumber string `json:"id_number"`
					FullName string `json:"full_name"`
					DOB      string `json:"dob"`
				} `json:"data"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal error: %v (raw: %s)", err, string(msg))
		}
		if env.Event != string(ws.EventOCRCompleted) {
			t.Errorf("expected event %s, got %s", ws.EventOCRCompleted, env.Event)
		}
		if env.Payload.TaskID != "task-uuid-abc-123" {
			t.Errorf("expected task_id task-uuid-abc-123, got %s", env.Payload.TaskID)
		}
		if env.Payload.Data.IDNumber != "040203000001" || env.Payload.Data.FullName != "NGUYỄN VĂN A" {
			t.Errorf("unexpected extracted text data: %+v", env.Payload.Data)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for target client to receive ocr_completed event")
	}

	// Other client must NOT receive this event
	select {
	case msg := <-clientOther.SendChan():
		t.Fatalf("other client unexpectedly received message: %s", string(msg))
	case <-time.After(50 * time.Millisecond):
		// Success: no leak to other users
	}
}
