package ws

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hskvinh/backend/internal/middleware"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 2048

	// Time allowed for first-message authentication when no ticket is in query.
	firstMessageAuthWait = 5 * time.Second
)

// Client represents a single WebSocket connection managed by the Hub,
// tagged with UserID, Role, and JWTExpiresAt extracted from the authenticated session.
type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	sendOnce     sync.Once
	closeOnce    sync.Once
	writeMu      sync.Mutex
	UserID       int64
	Role         string
	JWTExpiresAt time.Time
}

// NewClient creates a Client with a 256-message send buffer and registers it with the hub.
func NewClient(hub *Hub, conn *websocket.Conn, userID int64, role string, jwtExpiresAt ...time.Time) *Client {
	var exp time.Time
	if len(jwtExpiresAt) > 0 && !jwtExpiresAt[0].IsZero() {
		exp = jwtExpiresAt[0]
	} else {
		exp = time.Now().UTC().Add(24 * time.Hour)
	}

	client := &Client{
		hub:          hub,
		conn:         conn,
		send:         make(chan []byte, ClientSendBufferSize),
		UserID:       userID,
		Role:         role,
		JWTExpiresAt: exp,
	}
	if hub != nil {
		hub.RegisterClient(client)
	}
	return client
}

// SendChan returns the client's outbound send channel (used by tests to inspect queued frames).
func (c *Client) SendChan() <-chan []byte {
	return c.send
}

func (c *Client) closeSendOnce() {
	c.sendOnce.Do(func() {
		close(c.send)
	})
}

// CloseWithCode sends a WebSocket Close control frame with a custom close code (e.g. 4001, 4002, 4003)
// before closing the send channel and underlying connection so peers reliably receive the close code.
func (c *Client) CloseWithCode(code int, reason string) {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		if c.conn != nil {
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			msg := websocket.FormatCloseMessage(code, reason)
			_ = c.conn.WriteMessage(websocket.CloseMessage, msg)
		}
		c.writeMu.Unlock()

		c.closeSendOnce()

		c.writeMu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.writeMu.Unlock()
	})
}

// ReadPump reads messages from the WebSocket (we only care about pong/close after handshake).
func (c *Client) ReadPump() {
	defer func() {
		if c.hub != nil {
			c.hub.UnregisterClient(c)
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}()
	if c.conn == nil {
		return
	}
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				CloseCodeTokenExpired,
				CloseCodeMaxConnEvicted,
				CloseCodeSessionTerminated,
			) {
				log.Printf("[ws] read error: %v", err)
			}
			break
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection, sends periodic pings,
// and actively watches JWT expiration and account revocation to close with 4001 / 4003.
func (c *Client) WritePump() {
	if c.conn == nil {
		return
	}
	ticker := time.NewTicker(pingPeriod)
	ttl := time.Until(c.JWTExpiresAt)
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	expTimer := time.NewTimer(ttl)

	defer func() {
		ticker.Stop()
		expTimer.Stop()
		if c.hub != nil {
			c.hub.UnregisterClient(c)
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}()

	for {
		select {
		case <-expTimer.C:
			// JWT token expired while WebSocket was open -> close with 4001 so Frontend refreshes token
			c.CloseWithCode(CloseCodeTokenExpired, "JWT token expired")
			return

		case message, ok := <-c.send:
			if !ok {
				return
			}
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				c.writeMu.Unlock()
				return
			}
			_, _ = w.Write(message)

			// Drain queued messages into separate frames to keep each JSON message clean
			if err := w.Close(); err != nil {
				c.writeMu.Unlock()
				return
			}
			c.writeMu.Unlock()

		case <-ticker.C:
			if c.UserID > 0 && middleware.IsAccountTokenRevoked(context.Background(), c.UserID) {
				c.CloseWithCode(CloseCodeSessionTerminated, "Account session revoked or disabled")
				return
			}
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
