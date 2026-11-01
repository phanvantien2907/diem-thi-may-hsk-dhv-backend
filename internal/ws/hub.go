package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"
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
)

// Event là JSON payload được đẩy tới mọi client đã kết nối.
type Event struct {
	Type      EventType   `json:"type"`
	Payload   interface{} `json:"payload"`
	Timestamp string      `json:"timestamp"`
}

// Hub quản lý tập hợp các client WebSocket đang hoạt động và phát broadcast các event.
type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

// NewHub tạo và trả về một Hub mới.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main event loop. Must be called in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
			log.Printf("[ws] client connected (total=%d)", h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[ws] client disconnected (total=%d)", h.ClientCount())

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client too slow, drop it
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast serializes an Event and pushes it to all connected clients.
func (h *Hub) Broadcast(eventType EventType, payload interface{}) {
	evt := Event{
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return
	}
	h.broadcast <- data
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
