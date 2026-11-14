package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/ws"
	"github.com/redis/go-redis/v9"
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
				Event  string `json:"event"`
				TaskID string `json:"task_id"`
				Status string `json:"status"`
				Data   struct {
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

// startFakeRedisPubSubPipe starts a minimal in-process RESP pipe that accepts SUBSCRIBE
// and pushes the given payloads as Redis Pub/Sub messages on channel "cccd:ocr_events" without opening TCP sockets.
func startFakeRedisPubSubPipe(t *testing.T, channel string, payloadsChan <-chan string) (func(ctx context.Context, network, addr string) (net.Conn, error), <-chan struct{}, func()) {
	t.Helper()
	stopChan := make(chan struct{})
	subscribed := make(chan struct{})
	var subOnce atomic.Bool
	dialer := func(_ context.Context, _, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		writeCh := make(chan string, 32)
		go func() {
			for msg := range writeCh {
				_, _ = serverConn.Write([]byte(msg))
			}
		}()
		go func() {
			defer serverConn.Close()
			defer close(writeCh)
			reader := bufio.NewReader(serverConn)
			for {
				header, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				header = strings.TrimSpace(header)
				if !strings.HasPrefix(header, "*") {
					continue
				}
				argc, _ := strconv.Atoi(header[1:])
				args := make([]string, 0, argc)
				for i := 0; i < argc; i++ {
					_, _ = reader.ReadString('\n') // $<len>
					arg, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					args = append(args, strings.TrimSpace(arg))
				}
				if len(args) > 0 && strings.EqualFold(args[0], "SUBSCRIBE") {
					writeCh <- fmt.Sprintf("*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:1\r\n", len(channel), channel)
					if subOnce.CompareAndSwap(false, true) {
						close(subscribed)
					}
					break
				}
				if len(args) > 0 && strings.EqualFold(args[0], "HELLO") {
					writeCh <- "-ERR unknown command\r\n"
					continue
				}
				if len(args) > 0 && strings.EqualFold(args[0], "PING") {
					writeCh <- "+PONG\r\n"
					continue
				}
				writeCh <- "+OK\r\n"
			}

			for payload := range payloadsChan {
				writeCh <- fmt.Sprintf("*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
					len(channel), channel, len(payload), payload)
			}
			<-stopChan
		}()
		return clientConn, nil
	}
	return dialer, subscribed, func() {
		close(stopChan)
	}
}

// H-02: Verify that when WorkerPool dispatches locally via hub.SendToUser AND publishes
// a Zero-PII signal (Data: nil) to Redis Pub/Sub on the same instance, ListenRedisPubSub
// does NOT duplicate events or clobber ocr_completed with Data: null.
func TestHub_H02_RedisPubSubNoDuplicateOrNullDataClobber(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()

	targetUserID := int64(999)
	clientTarget := ws.NewClient(hub, nil, targetUserID, "candidate")
	time.Sleep(20 * time.Millisecond)

	pubPayloads := make(chan string, 4)
	dialer, subscribed, stopSrv := startFakeRedisPubSubPipe(t, "cccd:ocr_events", pubPayloads)
	defer stopSrv()

	rdb := redis.NewClient(&redis.Options{
		Addr:             "pipe:0",
		Protocol:         2,
		DisableIndentity: true,
		Dialer:           dialer,
	})
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubsub := rdb.Subscribe(ctx, "cccd:ocr_events")
	go hub.ListenRedisPubSub(ctx, pubsub)
	select {
	case <-subscribed:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for SUBSCRIBE")
	}

	// 1. Simulate WorkerPool.emitEvent for ocr_completed:
	// First, local dispatcher sends full PII data via hub.SendToUser
	localCompleted := map[string]interface{}{
		"event":    "ocr_completed",
		"task_id":  "task-dup-h02",
		"user_id":  targetUserID,
		"status":   "COMPLETED",
		"progress": 100,
		"data": map[string]interface{}{
			"id_number": "001099000001",
			"full_name": "NGUYỄN VĂN A",
		},
	}
	hub.SendToUser(targetUserID, ws.EventOCRCompleted, localCompleted)

	// Second, Redis Pub/Sub receives the Zero-PII signal (no "data" field)
	pubPayloads <- `{"event":"ocr_completed","task_id":"task-dup-h02","user_id":999,"status":"COMPLETED","progress":100,"timestamp":"2026-11-14T12:00:00Z"}`
	close(pubPayloads)

	time.Sleep(80 * time.Millisecond)

	// Drain clientTarget.SendChan(): there must be EXACTLY 1 message, and its data must be non-null!
	var received [][]byte
	for {
		select {
		case msg := <-clientTarget.SendChan():
			received = append(received, msg)
		default:
			goto doneDrain
		}
	}
doneDrain:
	if len(received) != 1 {
		t.Fatalf("expected exactly 1 WebSocket message (no Redis duplicate/clobber), got %d messages", len(received))
	}
}

// H-06: Verify multi-tab disconnect only fires when the LAST tab closes,
// and slow-client eviction on the last tab also fires OnUserDisconnected.
func TestHub_H06_SlowClientEvictionAndMultiTabDisconnect(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()

	var disconnectCount atomic.Int32
	var lastDisconnectedUser atomic.Int64
	hub.AddOnUserDisconnected(func(userID int64) {
		disconnectCount.Add(1)
		lastDisconnectedUser.Store(userID)
	})

	// 1. Multi-tab user (2 tabs): closing tab 1 must NOT trigger disconnect
	tab1 := ws.NewClient(hub, nil, 701, "candidate")
	tab2 := ws.NewClient(hub, nil, 701, "candidate")
	hub.UnregisterClient(tab1)
	if disconnectCount.Load() != 0 {
		t.Fatalf("closing 1 of 2 tabs must not fire disconnect callback, got count=%d", disconnectCount.Load())
	}
	hub.UnregisterClient(tab2)
	if disconnectCount.Load() != 1 || lastDisconnectedUser.Load() != 701 {
		t.Fatalf("closing last tab must fire disconnect callback once for user 701, got count=%d user=%d", disconnectCount.Load(), lastDisconnectedUser.Load())
	}

	// 2. Slow-client eviction on user's only connection must also fire disconnect callback
	slowClient := ws.NewClient(hub, nil, 702, "candidate")
	_ = slowClient
	for i := 0; i <= ws.ClientSendBufferSize+2; i++ {
		hub.SendToUser(702, ws.EventOCRProgress, map[string]any{"seq": i})
	}
	if hub.UserConnectionCount(702) != 0 {
		t.Fatalf("expected slow client for user 702 to be evicted, got %d conns", hub.UserConnectionCount(702))
	}
	if disconnectCount.Load() != 2 || lastDisconnectedUser.Load() != 702 {
		t.Fatalf("expected slow-client eviction of last connection to fire disconnect callback for user 702, got count=%d lastUser=%d",
			disconnectCount.Load(), lastDisconnectedUser.Load())
	}
}

// H-06b: Verify WebSocket disconnect grace period preserves in-flight OCR tasks on brief 4G reconnects,
// fires once if the user remains offline past the grace window, fires immediately on forced DisconnectUser,
// and cleans up pending timers on Hub.Stop().
func TestHub_H06b_DisconnectGracePeriodAndReconnect(t *testing.T) {
	hub := ws.NewHub()
	hub.SetDisconnectGracePeriod(150 * time.Millisecond)
	go hub.Run()
	defer hub.Stop()

	var disconnectCount atomic.Int32
	var lastDisconnectedUser atomic.Int64
	hub.AddOnUserDisconnected(func(userID int64) {
		disconnectCount.Add(1)
		lastDisconnectedUser.Store(userID)
	})

	// 1. Brief disconnect + reconnect within grace period (40ms < 150ms) -> callback MUST NOT fire
	c1 := ws.NewClient(hub, nil, 801, "candidate")
	hub.UnregisterClient(c1)
	if disconnectCount.Load() != 0 {
		t.Fatalf("expected disconnect callback to wait for grace period, but fired immediately")
	}
	time.Sleep(40 * time.Millisecond)
	c1Reconnected := ws.NewClient(hub, nil, 801, "candidate")
	time.Sleep(180 * time.Millisecond)
	if disconnectCount.Load() != 0 {
		t.Fatalf("expected reconnect within grace period to cancel timer, got count=%d", disconnectCount.Load())
	}

	// 2. Final disconnect with no reconnect -> callback fires once after grace period
	hub.UnregisterClient(c1Reconnected)
	time.Sleep(50 * time.Millisecond)
	if disconnectCount.Load() != 0 {
		t.Fatalf("expected callback not to fire at 50ms (< 150ms grace), got count=%d", disconnectCount.Load())
	}
	time.Sleep(160 * time.Millisecond)
	if disconnectCount.Load() != 1 || lastDisconnectedUser.Load() != 801 {
		t.Fatalf("expected disconnect callback to fire once for user 801 after grace period, got count=%d user=%d",
			disconnectCount.Load(), lastDisconnectedUser.Load())
	}

	// 3. Forced DisconnectUser (logout / account deactivation) -> fires immediately despite grace period
	_ = ws.NewClient(hub, nil, 802, "candidate")
	evicted := hub.DisconnectUser(802, ws.CloseCodeSessionTerminated, "Account deactivated")
	if evicted != 1 {
		t.Fatalf("expected 1 evicted connection for user 802, got %d", evicted)
	}
	if disconnectCount.Load() != 2 || lastDisconnectedUser.Load() != 802 {
		t.Fatalf("expected forced DisconnectUser to fire callback immediately for user 802, got count=%d user=%d",
			disconnectCount.Load(), lastDisconnectedUser.Load())
	}

	// 4. Flapping (disconnect -> reconnect -> disconnect) restarts the grace window
	var flapCount atomic.Int32
	flapHub := ws.NewHub()
	flapHub.SetDisconnectGracePeriod(160 * time.Millisecond)
	go flapHub.Run()
	defer flapHub.Stop()
	flapHub.AddOnUserDisconnected(func(userID int64) {
		if userID == 804 {
			flapCount.Add(1)
		}
	})
	f1 := ws.NewClient(flapHub, nil, 804, "candidate")
	flapHub.UnregisterClient(f1)
	time.Sleep(100 * time.Millisecond)
	f2 := ws.NewClient(flapHub, nil, 804, "candidate")
	// Even 100ms after f2 connects (200ms after first disconnect), callback must NOT fire because reconnect cancelled old timer
	time.Sleep(100 * time.Millisecond)
	if flapCount.Load() != 0 {
		t.Fatalf("expected flapping reconnect to cancel first timer, got count=%d", flapCount.Load())
	}
	flapHub.UnregisterClient(f2)
	// Must still wait a fresh 160ms grace window from f2 disconnect
	time.Sleep(80 * time.Millisecond)
	if flapCount.Load() != 0 {
		t.Fatalf("expected fresh 160ms grace window from second disconnect, fired early at 80ms")
	}
	time.Sleep(140 * time.Millisecond)
	if flapCount.Load() != 1 {
		t.Fatalf("expected callback to fire exactly once after restarted grace window, got %d", flapCount.Load())
	}

	// 5. Slow-client eviction followed by reconnect within grace period cancels pending disconnect
	slow805 := ws.NewClient(hub, nil, 805, "candidate")
	_ = slow805
	for i := 0; i <= ws.ClientSendBufferSize+2; i++ {
		hub.SendToUser(805, ws.EventOCRProgress, map[string]any{"seq": i})
	}
	time.Sleep(40 * time.Millisecond)
	_ = ws.NewClient(hub, nil, 805, "candidate")
	time.Sleep(180 * time.Millisecond)
	if disconnectCount.Load() != 2 {
		t.Fatalf("expected reconnect within grace after slow-client eviction to preserve tasks, got count=%d lastUser=%d",
			disconnectCount.Load(), lastDisconnectedUser.Load())
	}

	// 6. Hub.Stop() cancels any pending disconnect timer so it never fires after shutdown
	c3 := ws.NewClient(hub, nil, 803, "candidate")
	hub.UnregisterClient(c3)
	hub.Stop()
	time.Sleep(200 * time.Millisecond)
	if disconnectCount.Load() != 2 {
		t.Fatalf("expected Hub.Stop() to cancel pending timer for user 803, got count=%d", disconnectCount.Load())
	}
}
