package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// EventType identifies the kind of real-time event.
type EventType string

const (
	EventSeatUpdated             EventType = "seat_updated"              // Ghế thay đổi trạng thái (held/available/booked)
	EventRegistrationCreated     EventType = "registration_created"      // Thí sinh vừa đăng ký (giữ ghế)
	EventRegistrationCancelled   EventType = "registration_cancelled"    // Đơn đăng ký bị hủy (hết hạn / thí sinh hủy)
	EventRegistrationTransferred EventType = "registration_transferred"  // Đơn đăng ký chuyển ca thi
	EventPaymentConfirmed        EventType = "payment_confirmed"         // Thanh toán thành công → ghế chốt
	EventSeatsReleased           EventType = "seats_released"            // CronJob vừa giải phóng ghế quá hạn
	EventAccountUpdated          EventType = "account_updated"           // Thông tin tài khoản (profile/password) vừa cập nhật
	EventDashboardMetricsUpdated EventType = "dashboard_metrics_updated" // Background job cập nhật dữ liệu dashboard
	EventTransactionException    EventType = "transaction_exception"     // Phát hiện giao dịch ngoại lệ
	EventNotificationPayload     EventType = "NOTIFICATION_PAYLOAD"      // Thông báo thời gian thực gửi tới người dùng
	EventNotificationRead        EventType = "notification_read"         // Đồng bộ trạng thái đã đọc giữa nhiều tab của cùng 1 user
	EventProfileStatusUpdated    EventType = "PROFILE_STATUS_UPDATED"    // Trạng thái thẩm định hồ sơ thí sinh thay đổi (verified/pending/rejected)
	EventForceLogout             EventType = "FORCE_LOGOUT"              // Cưỡng chế đăng xuất ngay lập tức khi tài khoản bị vô hiệu hóa
	EventShiftChanged            EventType = "SHIFT_CHANGED"             // Ca thi của thí sinh được quản trị viên điều chuyển
	EventCCCDExtracted           EventType = "CCCD_EXTRACTED"            // Kết quả trích xuất OCR/AI CCCD bất đồng bộ hoàn tất
	EventOCRProgress             EventType = "ocr_progress"              // Tiến trình xử lý OCR (QUEUED, PROCESSING)
	EventOCRCompleted            EventType = "ocr_completed"             // Trích xuất văn bản CCCD thành công
	EventOCRFailed               EventType = "ocr_failed"                // Trích xuất văn bản CCCD thất bại
)

// Resilience, Buffer, and Custom WebSocket Close Code constants.
const (
	// MaxConnectionsPerUser limits simultaneous open WebSocket tabs/devices per account_id.
	MaxConnectionsPerUser = 5

	// ClientSendBufferSize is the per-client outbound message channel capacity.
	// Slow clients whose buffer fills up are dropped immediately to prevent memory bloat.
	ClientSendBufferSize = 256

	// DefaultThrottleWindow is the debounce/batch interval for high-frequency broadcast storms.
	DefaultThrottleWindow = 250 * time.Millisecond

	// Custom WebSocket Close Codes (RFC 6455 private use range 4000-4999)
	CloseCodeTokenExpired      = 4001 // JWT expired or authentication failed
	CloseCodeMaxConnEvicted    = 4002 // Exceeded MaxConnectionsPerUser (oldest connection evicted)
	CloseCodeSessionTerminated = 4003 // User logged out or account disabled by admin
)

// Event represents the JSON envelope pushed to WebSocket clients.
// Includes both "type"/"payload" and "event"/"data" aliases so all existing
// and new frontend listeners work seamlessly.
type Event struct {
	Type      EventType   `json:"type"`
	Event     EventType   `json:"event"`
	Payload   interface{} `json:"payload"`
	Data      interface{} `json:"data"`
	Timestamp string      `json:"timestamp"`
}

type throttledItem struct {
	eventType EventType
	payload   interface{}
}

// Hub manages active WebSocket clients in a single-instance modular monolith.
// Maintains map[account_id][]*Client for O(1) targeted multi-tab delivery and max-5 connection enforcement.
type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]struct{}
	userClients map[int64][]*Client
	roleClients map[string]map[*Client]struct{}
	broadcast   chan []byte
	register    chan *Client
	unregister  chan *Client

	throttleMu          sync.Mutex
	throttleQueue       map[string]throttledItem
	throttleWindow      time.Duration
	tickets             *TicketStore
	onUserDisconnected  func(userID int64)
	disconnectListeners []func(userID int64)
	disconnectGrace     time.Duration
	pendingDisconnect   map[int64]*time.Timer
	stopCh              chan struct{}
	stopOnce            sync.Once
	recentOCRDispMu     sync.Mutex
	recentOCRDispatched map[string]time.Time
}

// SetDisconnectGracePeriod configures an optional grace window (e.g., 3s for mobile 4G reconnects)
// before firing user-disconnect callbacks when a user's last WebSocket connection closes.
// Setting d <= 0 keeps immediate disconnect callback execution.
func (h *Hub) SetDisconnectGracePeriod(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d < 0 {
		d = 0
	}
	h.disconnectGrace = d
}

// SetOnUserDisconnected registers a callback invoked when all WebSocket connections for a user are closed.
func (h *Hub) SetOnUserDisconnected(fn func(userID int64)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUserDisconnected = fn
}

// AddOnUserDisconnected appends an additional disconnect callback invoked when a user's last WebSocket connection closes.
func (h *Hub) AddOnUserDisconnected(fn func(userID int64)) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnectListeners = append(h.disconnectListeners, fn)
}

// NewHub creates and initializes a new Hub with a built-in TicketStore and throttle buffer.
func NewHub() *Hub {
	return &Hub{
		clients:             make(map[*Client]struct{}),
		userClients:         make(map[int64][]*Client),
		roleClients:         make(map[string]map[*Client]struct{}),
		broadcast:           make(chan []byte, 512),
		register:            make(chan *Client, 128),
		unregister:          make(chan *Client, 128),
		throttleQueue:       make(map[string]throttledItem),
		throttleWindow:      DefaultThrottleWindow,
		tickets:             NewTicketStore(),
		pendingDisconnect:   make(map[int64]*time.Timer),
		stopCh:              make(chan struct{}),
		recentOCRDispatched: make(map[string]time.Time),
	}
}

// Stop signals the Hub's Run loop to exit cleanly and cancels any pending disconnect timers.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
		h.mu.Lock()
		for uid, timer := range h.pendingDisconnect {
			if timer != nil {
				timer.Stop()
			}
			delete(h.pendingDisconnect, uid)
		}
		h.mu.Unlock()
	})
}

// TicketStore returns the Hub's one-time ticket manager.
func (h *Hub) TicketStore() *TicketStore {
	return h.tickets
}

// SetThrottleWindow overrides the debounce interval (useful for fast unit tests).
func (h *Hub) SetThrottleWindow(d time.Duration) {
	h.throttleMu.Lock()
	if d > 0 {
		h.throttleWindow = d
	}
	h.throttleMu.Unlock()
}

// RegisterClient synchronously registers a Client in the Hub, enforcing MaxConnectionsPerUser (5).
// If the user already has 5 active connections, the oldest connection (index 0) is evicted with close code 4002.
func (h *Hub) RegisterClient(client *Client) {
	if client == nil {
		return
	}
	var evicted []*Client

	h.mu.Lock()
	if client.UserID > 0 {
		if pendingTimer, ok := h.pendingDisconnect[client.UserID]; ok {
			if pendingTimer != nil {
				pendingTimer.Stop()
			}
			delete(h.pendingDisconnect, client.UserID)
			log.Printf("[ws] user %d reconnected within grace period; in-flight OCR tasks preserved", client.UserID)
		}
		conns := h.userClients[client.UserID]
		for len(conns) >= MaxConnectionsPerUser {
			oldest := conns[0]
			conns = conns[1:]
			h.userClients[client.UserID] = conns
			if _, exists := h.clients[oldest]; exists {
				delete(h.clients, oldest)
				if oldest.Role != "" {
					if rset, ok := h.roleClients[oldest.Role]; ok {
						delete(rset, oldest)
						if len(rset) == 0 {
							delete(h.roleClients, oldest.Role)
						}
					}
				}
				evicted = append(evicted, oldest)
			}
		}
		h.userClients[client.UserID] = append(conns, client)
	}

	h.clients[client] = struct{}{}
	if client.Role != "" {
		if h.roleClients[client.Role] == nil {
			h.roleClients[client.Role] = make(map[*Client]struct{})
		}
		h.roleClients[client.Role][client] = struct{}{}
	}
	total := len(h.clients)
	h.mu.Unlock()

	for _, old := range evicted {
		old.CloseWithCode(CloseCodeMaxConnEvicted, "Exceeded maximum 5 concurrent connections; oldest tab evicted")
	}

	log.Printf("[ws] client connected (user_id=%d, role=%s, total=%d)", client.UserID, client.Role, total)
}

// UnregisterClient removes a Client from the Hub safely and triggers user disconnect hooks
// when the user's last active WebSocket connection closes (honoring disconnectGrace if > 0).
func (h *Hub) UnregisterClient(client *Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	removed := h.removeClientLocked(client)
	userID := client.UserID
	lastConnClosed := removed && userID > 0 && len(h.userClients[userID]) == 0
	total := len(h.clients)
	h.mu.Unlock()
	if removed {
		log.Printf("[ws] client disconnected (user_id=%d, role=%s, total=%d)", client.UserID, client.Role, total)
		if lastConnClosed {
			h.scheduleUserDisconnect(userID)
		}
	}
}

func (h *Hub) fireDisconnectListeners(userID int64) {
	if userID <= 0 {
		return
	}
	h.mu.RLock()
	onDisconnect := h.onUserDisconnected
	var listeners []func(userID int64)
	if len(h.disconnectListeners) > 0 {
		listeners = make([]func(userID int64), len(h.disconnectListeners))
		copy(listeners, h.disconnectListeners)
	}
	h.mu.RUnlock()

	if onDisconnect != nil {
		onDisconnect(userID)
	}
	for _, fn := range listeners {
		if fn != nil {
			fn(userID)
		}
	}
}

func (h *Hub) scheduleUserDisconnect(userID int64) {
	if userID <= 0 {
		return
	}
	h.mu.Lock()
	grace := h.disconnectGrace
	if grace <= 0 {
		if prev := h.pendingDisconnect[userID]; prev != nil {
			prev.Stop()
			delete(h.pendingDisconnect, userID)
		}
		h.mu.Unlock()
		h.fireDisconnectListeners(userID)
		return
	}
	if prev := h.pendingDisconnect[userID]; prev != nil {
		prev.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(grace, func() {
		h.mu.Lock()
		current, ok := h.pendingDisconnect[userID]
		if !ok || current != timer {
			h.mu.Unlock()
			return
		}
		delete(h.pendingDisconnect, userID)
		stillOffline := len(h.userClients[userID]) == 0
		select {
		case <-h.stopCh:
			h.mu.Unlock()
			return
		default:
		}
		h.mu.Unlock()
		if stillOffline {
			h.fireDisconnectListeners(userID)
		}
	})
	h.pendingDisconnect[userID] = timer
	h.mu.Unlock()
}

func (h *Hub) removeClientLocked(client *Client) bool {
	if _, ok := h.clients[client]; !ok {
		return false
	}
	delete(h.clients, client)

	if client.UserID > 0 {
		conns := h.userClients[client.UserID]
		filtered := conns[:0]
		for _, c := range conns {
			if c != client {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			delete(h.userClients, client.UserID)
		} else {
			h.userClients[client.UserID] = filtered
		}
	}

	if client.Role != "" {
		if set, exists := h.roleClients[client.Role]; exists {
			delete(set, client)
			if len(set) == 0 {
				delete(h.roleClients, client.Role)
			}
		}
	}

	client.closeSendOnce()
	return true
}

// Run starts the hub's main event loop and high-frequency broadcast throttle ticker.
// Must be called in a goroutine.
func (h *Hub) Run() {
	h.throttleMu.Lock()
	window := h.throttleWindow
	h.throttleMu.Unlock()
	if window <= 0 {
		window = DefaultThrottleWindow
	}
	throttleTicker := time.NewTicker(window)
	defer throttleTicker.Stop()

	for {
		select {
		case <-h.stopCh:
			return

		case client := <-h.register:
			h.RegisterClient(client)

		case client := <-h.unregister:
			h.UnregisterClient(client)

		case message := <-h.broadcast:
			h.dispatchBroadcastBytes(message)

		case <-throttleTicker.C:
			h.FlushThrottledBroadcasts()
		}
	}
}

func (h *Hub) notifyDisconnectedUsers(userIDs []int64) {
	if len(userIDs) == 0 {
		return
	}
	for _, uid := range userIDs {
		if uid <= 0 {
			continue
		}
		h.scheduleUserDisconnect(uid)
	}
}

func (h *Hub) dispatchBroadcastBytes(message []byte) {
	var slowClients []*Client
	var disconnectedUsers []int64

	h.mu.Lock()
	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			// Client send channel buffer (256) is full -> drop slow client immediately
			if h.removeClientLocked(client) && client.UserID > 0 && len(h.userClients[client.UserID]) == 0 {
				disconnectedUsers = append(disconnectedUsers, client.UserID)
			}
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()

	for _, sc := range slowClients {
		if sc.conn != nil {
			_ = sc.conn.Close()
		}
	}
	h.notifyDisconnectedUsers(disconnectedUsers)
}

func marshalEvent(eventType EventType, payload interface{}) ([]byte, bool) {
	evt := Event{
		Type:      eventType,
		Event:     eventType,
		Payload:   payload,
		Data:      payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return nil, false
	}
	return data, true
}

// Broadcast serializes an Event and pushes it to all connected clients.
// High-frequency seat events are automatically coalesced when BroadcastThrottled is used.
func (h *Hub) Broadcast(eventType EventType, payload interface{}) {
	data, ok := marshalEvent(eventType, payload)
	if !ok {
		return
	}
	select {
	case h.broadcast <- data:
	default:
		log.Printf("[ws] broadcast channel full, dropping event %s", eventType)
	}
}

// BroadcastThrottled batches/debounces high-frequency broadcast events (such as seat_updated
// during registration rush) by coalesceKey within a 200-500ms window before broadcasting.
func (h *Hub) BroadcastThrottled(eventType EventType, coalesceKey string, payload interface{}) {
	key := string(eventType) + ":" + coalesceKey
	h.throttleMu.Lock()
	h.throttleQueue[key] = throttledItem{
		eventType: eventType,
		payload:   payload,
	}
	h.throttleMu.Unlock()
}

// FlushThrottledBroadcasts flushes all pending coalesced high-frequency events immediately.
func (h *Hub) FlushThrottledBroadcasts() {
	h.throttleMu.Lock()
	if len(h.throttleQueue) == 0 {
		h.throttleMu.Unlock()
		return
	}
	pending := h.throttleQueue
	h.throttleQueue = make(map[string]throttledItem)
	h.throttleMu.Unlock()

	for _, item := range pending {
		data, ok := marshalEvent(item.eventType, item.payload)
		if !ok {
			continue
		}
		h.dispatchBroadcastBytes(data)
	}
}

// SendToUser pushes an event strictly to the active WebSocket connections in map[account_id][]*Client.
// Never broadcasts to other users. Drops any slow client whose 256-message buffer is full.
func (h *Hub) SendToUser(userID int64, eventType EventType, payload interface{}) {
	if userID <= 0 {
		return
	}
	data, ok := marshalEvent(eventType, payload)
	if !ok {
		return
	}
	h.recordLocalOCRDispatch(eventType, data)

	var slowClients []*Client
	var disconnectedUsers []int64

	h.mu.Lock()
	conns := h.userClients[userID]
	if len(conns) == 0 {
		h.mu.Unlock()
		return
	}
	// Copy slice snapshot to iterate safely even if a slow client is removed
	snapshot := make([]*Client, len(conns))
	copy(snapshot, conns)

	for _, client := range snapshot {
		select {
		case client.send <- data:
		default:
			if h.removeClientLocked(client) && client.UserID > 0 && len(h.userClients[client.UserID]) == 0 {
				disconnectedUsers = append(disconnectedUsers, client.UserID)
			}
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()

	for _, sc := range slowClients {
		if sc.conn != nil {
			_ = sc.conn.Close()
		}
	}
	h.notifyDisconnectedUsers(disconnectedUsers)
}

// SendToUsers pushes an event to all active WebSocket connections belonging to any user in userIDs.
func (h *Hub) SendToUsers(userIDs []int64, eventType EventType, payload interface{}) {
	if len(userIDs) == 0 {
		return
	}
	data, ok := marshalEvent(eventType, payload)
	if !ok {
		return
	}

	var slowClients []*Client
	var disconnectedUsers []int64

	h.mu.Lock()
	seen := make(map[int64]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid <= 0 {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		conns := h.userClients[uid]
		if len(conns) == 0 {
			continue
		}
		snapshot := make([]*Client, len(conns))
		copy(snapshot, conns)
		for _, client := range snapshot {
			select {
			case client.send <- data:
			default:
				if h.removeClientLocked(client) && client.UserID > 0 && len(h.userClients[client.UserID]) == 0 {
					disconnectedUsers = append(disconnectedUsers, client.UserID)
				}
				slowClients = append(slowClients, client)
			}
		}
	}
	h.mu.Unlock()

	for _, sc := range slowClients {
		if sc.conn != nil {
			_ = sc.conn.Close()
		}
	}
	h.notifyDisconnectedUsers(disconnectedUsers)
}

// SendToRole pushes an event to all active WebSocket connections matching the given role.
func (h *Hub) SendToRole(role string, eventType EventType, payload interface{}) {
	if role == "" {
		return
	}
	data, ok := marshalEvent(eventType, payload)
	if !ok {
		return
	}

	var slowClients []*Client
	var disconnectedUsers []int64

	h.mu.Lock()
	conns, exists := h.roleClients[role]
	if !exists {
		h.mu.Unlock()
		return
	}
	for client := range conns {
		select {
		case client.send <- data:
		default:
			if h.removeClientLocked(client) && client.UserID > 0 && len(h.userClients[client.UserID]) == 0 {
				disconnectedUsers = append(disconnectedUsers, client.UserID)
			}
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()

	for _, sc := range slowClients {
		if sc.conn != nil {
			_ = sc.conn.Close()
		}
	}
	h.notifyDisconnectedUsers(disconnectedUsers)
}

// DisconnectUser immediately ranges over and terminates all active WebSocket connections
// of an account_id (e.g., when the user logs out or is disabled by an admin).
func (h *Hub) DisconnectUser(userID int64, closeCode int, reason string) int {
	if userID <= 0 {
		return 0
	}
	if closeCode == 0 {
		closeCode = CloseCodeSessionTerminated
	}
	if reason == "" {
		reason = "Session terminated"
	}

	h.mu.Lock()
	hadPendingTimer := false
	if pendingTimer, ok := h.pendingDisconnect[userID]; ok {
		hadPendingTimer = true
		if pendingTimer != nil {
			pendingTimer.Stop()
		}
		delete(h.pendingDisconnect, userID)
	}
	conns := h.userClients[userID]
	if len(conns) == 0 {
		h.mu.Unlock()
		if hadPendingTimer {
			h.fireDisconnectListeners(userID)
		}
		return 0
	}
	snapshot := make([]*Client, len(conns))
	copy(snapshot, conns)
	delete(h.userClients, userID)
	for _, client := range snapshot {
		delete(h.clients, client)
		if client.Role != "" {
			if set, exists := h.roleClients[client.Role]; exists {
				delete(set, client)
				if len(set) == 0 {
					delete(h.roleClients, client.Role)
				}
			}
		}
	}
	h.mu.Unlock()

	for _, client := range snapshot {
		client.CloseWithCode(closeCode, reason)
	}

	h.mu.RLock()
	onDisconnect := h.onUserDisconnected
	listeners := make([]func(userID int64), len(h.disconnectListeners))
	copy(listeners, h.disconnectListeners)
	h.mu.RUnlock()

	if onDisconnect != nil {
		onDisconnect(userID)
	}
	for _, fn := range listeners {
		if fn != nil {
			fn(userID)
		}
	}
	return len(snapshot)
}

// ClientCount returns the total number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// UserConnectionCount returns the number of active connections for a specific userID.
func (h *Hub) UserConnectionCount(userID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.userClients[userID])
}

func (h *Hub) recordLocalOCRDispatch(eventType EventType, marshaled []byte) {
	switch eventType {
	case EventOCRProgress, EventOCRCompleted, EventOCRFailed, EventType("ocr_back_completed"):
	default:
		return
	}
	var env struct {
		Payload struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(marshaled, &env); err != nil || env.Payload.TaskID == "" {
		return
	}
	now := time.Now()
	key := string(eventType) + ":" + env.Payload.TaskID + ":" + env.Payload.Status
	h.recentOCRDispMu.Lock()
	if len(h.recentOCRDispatched) > 1024 {
		for k, ts := range h.recentOCRDispatched {
			if now.Sub(ts) > 3*time.Minute {
				delete(h.recentOCRDispatched, k)
			}
		}
	}
	h.recentOCRDispatched[key] = now
	h.recentOCRDispMu.Unlock()
}

func (h *Hub) wasOCRDispatchedLocally(event, taskID, status string) bool {
	if taskID == "" {
		return false
	}
	key := event + ":" + taskID + ":" + status
	h.recentOCRDispMu.Lock()
	defer h.recentOCRDispMu.Unlock()
	ts, exists := h.recentOCRDispatched[key]
	if !exists {
		return false
	}
	if time.Since(ts) > 3*time.Minute {
		delete(h.recentOCRDispatched, key)
		return false
	}
	return true
}

// ListenRedisPubSub subscribes to Redis Pub/Sub channels (e.g. cccd:ocr_events) and forwards
// incoming real-time OCR and system events to target user connections via SendToUser.
func (h *Hub) ListenRedisPubSub(ctx context.Context, pubsub *redis.PubSub) {
	if pubsub == nil {
		return
	}
	defer pubsub.Close()
	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg == nil || msg.Payload == "" {
				continue
			}

			var envelope struct {
				Event     string          `json:"event"`
				TaskID    string          `json:"task_id"`
				UserID    int64           `json:"user_id"`
				Status    string          `json:"status"`
				Progress  int             `json:"progress"`
				Data      json.RawMessage `json:"data,omitempty"`
				Error     string          `json:"error,omitempty"`
				Timestamp string          `json:"timestamp"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				log.Printf("[ws.hub] redis pubsub unmarshal error: %v (raw: %s)", err, msg.Payload)
				continue
			}

			// Never clobber WebSocket clients with a Zero-PII (Data: nil) completion signal from Redis,
			// and never re-send an OCR event that was already dispatched locally on this Hub instance.
			if envelope.Event == string(EventOCRCompleted) || envelope.Event == "ocr_back_completed" {
				if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
					continue
				}
			}
			if h.wasOCRDispatchedLocally(envelope.Event, envelope.TaskID, envelope.Status) {
				continue
			}

			if envelope.UserID > 0 && envelope.Event != "" {
				// Re-dispatch targeted event to all active client tabs for this user
				h.SendToUser(envelope.UserID, EventType(envelope.Event), envelope)
			}
		}
	}
}
