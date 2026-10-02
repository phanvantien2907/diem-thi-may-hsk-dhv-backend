package ws

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for WebSocket (CORS is handled at HTTP level).
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler manages the WebSocket upgrade endpoint.
type Handler struct {
	hub       *Hub
	jwtSecret string
}

// NewHandler creates a WebSocket Handler.
func NewHandler(hub *Hub, jwtSecret string) *Handler {
	return &Handler{hub: hub, jwtSecret: jwtSecret}
}

// ServeWS upgrades the HTTP connection to WebSocket.
// JWT token is passed via query parameter "token" (browsers cannot set headers on WS).
//
// Usage from frontend:
//
//	const ws = new WebSocket("ws://localhost:8080/ws?token=<jwt_access_token>");
func (h *Handler) ServeWS(c *gin.Context) {
	// ── Auth: validate JWT from query param ───────────────────────────────
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token query parameter"})
		return
	}

	_, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(h.jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	// ── Upgrade to WebSocket ──────────────────────────────────────────────
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	client := NewClient(h.hub, conn)
	go client.WritePump()
	go client.ReadPump()
}

// RegisterRoutes registers the WebSocket endpoint on the Gin engine (not in /api/v1 group).
func RegisterRoutes(engine *gin.Engine, handler *Handler) {
	engine.GET("/ws", handler.ServeWS)
}
