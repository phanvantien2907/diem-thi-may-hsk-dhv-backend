package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/ws"
)

const testJWTSecret = "ws-resilience-test-secret-key-32b"

func init() {
	gin.SetMode(gin.TestMode)
}

// pipeListener implements net.Listener using in-memory net.Pipe() connections
// so full WebSocket upgrades and close codes work in any sandbox without TCP loopback.
type pipeListener struct {
	ch     chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		ch:     make(chan net.Conn, 64),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}

func (l *pipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	serverConn, clientConn := net.Pipe()
	select {
	case l.ch <- serverConn:
		return clientConn, nil
	case <-l.closed:
		_ = serverConn.Close()
		_ = clientConn.Close()
		return nil, errors.New("listener closed")
	case <-ctx.Done():
		_ = serverConn.Close()
		_ = clientConn.Close()
		return nil, ctx.Err()
	}
}

func issueTestJWT(t *testing.T, userID int64, role string, ttl time.Duration) string {
	t.Helper()
	claims := middleware.Claims{
		UserID:   userID,
		UserRole: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign test jwt: %v", err)
	}
	return tok
}

func setupTestWSServer(t *testing.T) (*ws.Hub, *gin.Engine, *websocket.Dialer) {
	t.Helper()
	hub := ws.NewHub()
	go hub.Run()

	r := gin.New()
	h := ws.NewHandler(hub, testJWTSecret)
	ws.RegisterRoutes(r, h)

	pl := newPipeListener()
	srv := &http.Server{Handler: r}
	go func() {
		_ = srv.Serve(pl)
	}()

	t.Cleanup(func() {
		_ = pl.Close()
		_ = srv.Close()
	})

	dialer := &websocket.Dialer{
		NetDialContext:   pl.DialContext,
		HandshakeTimeout: 3 * time.Second,
	}
	return hub, r, dialer
}

// 1. Test Secure Handshake: Reject ?token= in URL, issue 30s One-Time Ticket, and forbid ticket reuse.
func TestTicketHandshake_And_RejectQueryToken(t *testing.T) {
	_, router, dialer := setupTestWSServer(t)
	jwtToken := issueTestJWT(t, 101, "candidate", time.Hour)

	// 1a. Passing ?token=<jwt> in WS URL must be rejected with HTTP 401
	_, resp, err := dialer.Dial("ws://inmemory/ws?token="+jwtToken, nil)
	if err == nil {
		t.Fatalf("expected dial with ?token= in URL to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized when ?token= is in URL, got %v", resp)
	}

	// 1b. Issue a One-Time Ticket via POST /api/v1/ws/ticket
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ws/ticket", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/v1/ws/ticket, got %d: %s", w.Code, w.Body.String())
	}

	var ticketEnvelope struct {
		Success bool `json:"success"`
		Data    struct {
			Ticket    string `json:"ticket"`
			ExpiresIn int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ticketEnvelope); err != nil {
		t.Fatalf("decode ticket response failed: %v", err)
	}
	if ticketEnvelope.Data.Ticket == "" || ticketEnvelope.Data.ExpiresIn != 30 {
		t.Fatalf("unexpected ticket response: %+v", ticketEnvelope)
	}

	// 1c. Connect using the valid one-time ticket
	wsURLWithTicket := "ws://inmemory/ws?ticket=" + ticketEnvelope.Data.Ticket
	conn, _, err := dialer.Dial(wsURLWithTicket, nil)
	if err != nil {
		t.Fatalf("expected dial with valid one-time ticket to succeed, got error: %v", err)
	}
	defer conn.Close()

	// 1d. Reusing the exact same ticket a second time must fail with 401 Unauthorized
	_, replayResp, replayErr := dialer.Dial(wsURLWithTicket, nil)
	if replayErr == nil {
		t.Fatalf("expected ticket replay to be rejected, but dial succeeded")
	}
	if replayResp == nil || replayResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on ticket replay, got %v", replayResp)
	}
}

// 2. Test Max 5 Connections Per User & FIFO Eviction with Close Code 4002.
func TestMaxConnectionsPerUser_EvictsOldest(t *testing.T) {
	hub, _, dialer := setupTestWSServer(t)
	const userID int64 = 202

	conns := make([]*websocket.Conn, 0, 6)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	evictErrCh := make(chan error, 1)

	for i := 0; i < ws.MaxConnectionsPerUser+1; i++ {
		entry, err := hub.TicketStore().IssueTicket(userID, "candidate", time.Now().Add(time.Hour), ws.DefaultTicketTTL)
		if err != nil {
			t.Fatalf("issue ticket %d failed: %v", i, err)
		}
		c, _, err := dialer.Dial("ws://inmemory/ws?ticket="+entry.Ticket, nil)
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		conns = append(conns, c)
		if i == 0 {
			go func(first *websocket.Conn) {
				_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _, rErr := first.ReadMessage()
				evictErrCh <- rErr
			}(c)
		}
	}

	deadline := time.Now().Add(time.Second)
	for hub.UserConnectionCount(userID) != ws.MaxConnectionsPerUser && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := hub.UserConnectionCount(userID); got != ws.MaxConnectionsPerUser {
		t.Fatalf("expected UserConnectionCount=%d after 6th connection, got %d", ws.MaxConnectionsPerUser, got)
	}

	// The 1st connection (conns[0]) must have been evicted with CloseCodeMaxConnEvicted (4002)
	err := <-evictErrCh
	if err == nil {
		t.Fatalf("expected evicted oldest connection to return close error")
	}
	closeErr, ok := err.(*websocket.CloseError)
	if !ok || closeErr.Code != ws.CloseCodeMaxConnEvicted {
		t.Fatalf("expected close code %d on evicted connection, got: %v", ws.CloseCodeMaxConnEvicted, err)
	}
}

// 3. Test Targeted Delivery: SendToUser delivers strictly to the target user's tabs and never to other users.
func TestTargetedDelivery_MultiTabIsolation(t *testing.T) {
	hub := ws.NewHub()

	userATab1 := ws.NewClient(hub, nil, 301, "candidate")
	userATab2 := ws.NewClient(hub, nil, 301, "candidate")
	userBTab1 := ws.NewClient(hub, nil, 302, "candidate")

	hub.SendToUser(301, ws.EventNotificationPayload, map[string]any{
		"id":      999,
		"user_id": 301,
		"title":   "Hồ sơ của bạn đã bị từ chối",
	})

	select {
	case msg := <-userATab1.SendChan():
		if !strings.Contains(string(msg), "Hồ sơ của bạn đã bị từ chối") {
			t.Fatalf("unexpected payload on userATab1: %s", string(msg))
		}
	default:
		t.Fatalf("expected userATab1 to receive targeted notification")
	}

	select {
	case msg := <-userATab2.SendChan():
		if !strings.Contains(string(msg), "Hồ sơ của bạn đã bị từ chối") {
			t.Fatalf("unexpected payload on userATab2: %s", string(msg))
		}
	default:
		t.Fatalf("expected userATab2 to receive targeted notification")
	}

	select {
	case msg := <-userBTab1.SendChan():
		t.Fatalf("expected userBTab1 to receive NOTHING, but got: %s", string(msg))
	default:
		// Pass: user B isolated
	}
}

// 4. Test Session Termination (DisconnectUser with Close Code 4003) & JWT Expiration (Close Code 4001).
func TestSessionTermination_And_TokenExpirationCloseCodes(t *testing.T) {
	hub, _, dialer := setupTestWSServer(t)

	// 4a. DisconnectUser closes all active WS clients of an account_id with 4003
	entry, _ := hub.TicketStore().IssueTicket(401, "candidate", time.Now().Add(time.Hour), ws.DefaultTicketTTL)
	conn1, _, err := dialer.Dial("ws://inmemory/ws?ticket="+entry.Ticket, nil)
	if err != nil {
		t.Fatalf("dial conn1 failed: %v", err)
	}
	defer conn1.Close()

	// Drain in background so net.Pipe write in CloseWithCode succeeds immediately
	readCh1 := make(chan error, 1)
	go func() {
		_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, rErr := conn1.ReadMessage()
		readCh1 <- rErr
	}()

	deadline := time.Now().Add(time.Second)
	for hub.UserConnectionCount(401) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	disconnected := hub.DisconnectUser(401, ws.CloseCodeSessionTerminated, "Account disabled by admin")
	if disconnected != 1 {
		t.Fatalf("expected 1 disconnected client, got %d", disconnected)
	}

	readErr := <-readCh1
	closeErr, ok := readErr.(*websocket.CloseError)
	if !ok || closeErr.Code != ws.CloseCodeSessionTerminated {
		t.Fatalf("expected close code %d (SessionTerminated), got %v", ws.CloseCodeSessionTerminated, readErr)
	}

	// 4b. Short-lived JWT expires while WS is connected -> closes with 4001
	expEntry, _ := hub.TicketStore().IssueTicket(402, "candidate", time.Now().Add(80*time.Millisecond), ws.DefaultTicketTTL)
	connExp, _, err := dialer.Dial("ws://inmemory/ws?ticket="+expEntry.Ticket, nil)
	if err != nil {
		t.Fatalf("dial connExp failed: %v", err)
	}
	defer connExp.Close()

	_ = connExp.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, expReadErr := connExp.ReadMessage()
	expCloseErr, ok := expReadErr.(*websocket.CloseError)
	if !ok || expCloseErr.Code != ws.CloseCodeTokenExpired {
		t.Fatalf("expected close code %d (TokenExpired) when JWT expires, got %v", ws.CloseCodeTokenExpired, expReadErr)
	}
}

// 5. Test Buffer Bloat Protection (256 message limit drops slow client) & BroadcastThrottled debouncing.
func TestSlowClientEviction_And_BroadcastThrottled(t *testing.T) {
	hub := ws.NewHub()
	slowClient := ws.NewClient(hub, nil, 501, "candidate")

	// Fill the 256-slot send channel completely
	for i := 0; i < ws.ClientSendBufferSize; i++ {
		hub.SendToUser(501, ws.EventNotificationPayload, map[string]any{"seq": i})
	}
	if hub.UserConnectionCount(501) != 1 {
		t.Fatalf("expected client to still be registered at exact capacity 256")
	}

	// The 257th message must detect buffer full and drop slowClient immediately
	hub.SendToUser(501, ws.EventNotificationPayload, map[string]any{"seq": 256})
	if hub.UserConnectionCount(501) != 0 {
		t.Fatalf("expected slow client to be evicted when buffer exceeded 256, got count=%d", hub.UserConnectionCount(501))
	}
	_ = slowClient

	// Test BroadcastThrottled coalesces multiple rapid seat updates for the same session/seat
	fastClient := ws.NewClient(hub, nil, 502, "candidate")
	for i := 0; i < 10; i++ {
		hub.BroadcastThrottled(ws.EventSeatUpdated, "session:10:seat:5", map[string]any{
			"session_id": 10,
			"seat_id":    5,
			"iteration":  i,
		})
	}
	hub.FlushThrottledBroadcasts()

	select {
	case msg := <-fastClient.SendChan():
		if !strings.Contains(string(msg), `"iteration":9`) {
			t.Fatalf("expected coalesced broadcast to contain latest state (iteration 9), got: %s", string(msg))
		}
	default:
		t.Fatalf("expected fastClient to receive flushed throttled broadcast")
	}

	// Ensure only 1 coalesced message was sent, not 10
	select {
	case extra := <-fastClient.SendChan():
		t.Fatalf("expected only 1 coalesced message, but received extra: %s", string(extra))
	default:
	}
}
