package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for WebSocket (CORS is handled at HTTP level).
	CheckOrigin: func(r *http.Request) bool { return true },
}

type firstAuthFrame struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

// Handler manages the WebSocket ticket endpoint and upgrade endpoint.
type Handler struct {
	hub       *Hub
	jwtSecret string
}

// NewHandler creates a WebSocket Handler.
func NewHandler(hub *Hub, jwtSecret string) *Handler {
	return &Handler{hub: hub, jwtSecret: jwtSecret}
}

// IssueTicket POST /api/v1/ws/ticket (and POST /ws/ticket)
// @Summary Cấp vé kết nối WebSocket dùng một lần (30s TTL)
// @Description Cấp One-Time Ticket có hiệu lực 30 giây để kết nối WebSocket mà không cần truyền JWT trên URL query.
// @Tags WebSocket
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực hoặc token hết hạn"
// @Router /ws/ticket [post]
func (h *Handler) IssueTicket(c *gin.Context) {
	uidVal, _ := c.Get(middleware.ContextKeyUserID)
	roleVal, _ := c.Get(middleware.ContextKeyUserRole)
	userID, _ := uidVal.(int64)
	userRole, _ := roleVal.(string)

	if userID <= 0 {
		utils.Unauthorized(c, "Chưa xác thực người dùng")
		return
	}

	// Extract exact JWT expiration from Authorization header if available
	jwtExp := time.Now().UTC().Add(24 * time.Hour)
	if authHeader := c.GetHeader("Authorization"); authHeader != "" {
		rawToken := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(authHeader, "Bearer "), "bearer "))
		claims := &middleware.Claims{}
		if tok, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
			return []byte(h.jwtSecret), nil
		}); err == nil && tok.Valid && claims.ExpiresAt != nil {
			jwtExp = claims.ExpiresAt.Time.UTC()
		}
	}

	entry, err := h.hub.TicketStore().IssueTicket(userID, userRole, jwtExp, DefaultTicketTTL)
	if err != nil {
		utils.InternalServerError(c, "Không thể khởi tạo vé kết nối WebSocket")
		return
	}

	utils.OK(c, gin.H{
		"ticket":     entry.Ticket,
		"expires_in": int(DefaultTicketTTL.Seconds()),
		"expires_at": entry.ExpiresAt.Format(time.RFC3339),
	})
}

// ServeWS upgrades the HTTP connection to WebSocket.
// SECURITY RULE: Passing `?token=<jwt>` in the URL is strictly rejected (HTTP 401) to prevent proxy/access log leaks.
// Supported authentication modes:
//  1. One-Time Ticket: `ws://host/ws?ticket=<30s_one_time_ticket>` (obtained from POST /api/v1/ws/ticket)
//  2. First-Message Auth: connect to `ws://host/ws` without query params and send `{"type":"auth","token":"<jwt>"}` as the first frame within 5s.
func (h *Handler) ServeWS(c *gin.Context) {
	// 1. Explicitly reject raw JWT in URL query string
	if c.Query("token") != "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Không chấp nhận truyền JWT qua tham số URL (?token=) để tránh lộ token trong log máy chủ. Vui lòng gọi POST /api/v1/ws/ticket hoặc xác thực qua tin nhắn đầu tiên.",
		})
		return
	}

	// 2. Mode A: One-Time Ticket in ?ticket=<ticket>
	if ticket := strings.TrimSpace(c.Query("ticket")); ticket != "" {
		entry, ok := h.hub.TicketStore().ConsumeTicket(ticket)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Vé kết nối WebSocket không hợp lệ, đã hết hạn (30s) hoặc đã được sử dụng"})
			return
		}
		if middleware.IsAccountTokenRevoked(c.Request.Context(), entry.UserID) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Tài khoản đã bị vô hiệu hóa hoặc thu hồi phiên đăng nhập"})
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[ws] upgrade error: %v", err)
			return
		}

		client := NewClient(h.hub, conn, entry.UserID, entry.Role, entry.JWTExpiresAt)
		go client.WritePump()
		go client.ReadPump()
		return
	}

	// 3. Mode B: First-Message Authentication over upgraded WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	go h.authenticateFirstMessage(conn)
}

func (h *Handler) authenticateFirstMessage(conn *websocket.Conn) {
	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(firstMessageAuthWait))

	_, rawMsg, err := conn.ReadMessage()
	if err != nil {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeTokenExpired, "First-message authentication timeout"),
		)
		_ = conn.Close()
		return
	}

	var frame firstAuthFrame
	if err := json.Unmarshal(rawMsg, &frame); err != nil || !strings.EqualFold(frame.Type, "auth") || strings.TrimSpace(frame.Token) == "" {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeTokenExpired, "First message must be {\"type\":\"auth\",\"token\":\"<jwt>\"}"),
		)
		_ = conn.Close()
		return
	}

	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(frame.Token), claims, func(t *jwt.Token) (any, error) {
		return []byte(h.jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid || claims.UserID <= 0 {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeTokenExpired, "Invalid or expired JWT token"),
		)
		_ = conn.Close()
		return
	}

	if middleware.IsAccountTokenRevoked(context.Background(), claims.UserID) {
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeSessionTerminated, "Account session revoked or disabled"),
		)
		_ = conn.Close()
		return
	}

	jwtExp := time.Now().UTC().Add(24 * time.Hour)
	if claims.ExpiresAt != nil {
		jwtExp = claims.ExpiresAt.Time.UTC()
	}

	client := NewClient(h.hub, conn, claims.UserID, claims.UserRole, jwtExp)
	go client.WritePump()
	go client.ReadPump()
}

// RegisterRoutes registers the WebSocket ticket and upgrade endpoints on both the /api/v1 RouterGroup and root Engine.
func RegisterRoutes(engine *gin.Engine, handler *Handler, apiGroups ...*gin.RouterGroup) {
	authMW := middleware.JWTAuth(handler.jwtSecret)

	// Root-level aliases (e.g. POST /ws/ticket and ws://host/ws?ticket=...)
	engine.POST("/ws/ticket", authMW, handler.IssueTicket)
	engine.GET("/ws", handler.ServeWS)

	// Explicitly register within the /api/v1 RouterGroup so POST /api/v1/ws/ticket never 404s
	if len(apiGroups) > 0 && apiGroups[0] != nil {
		api := apiGroups[0]
		api.POST("/ws/ticket", authMW, handler.IssueTicket)
		api.GET("/ws", handler.ServeWS)
	} else {
		v1 := engine.Group("/api/v1")
		v1.POST("/ws/ticket", authMW, handler.IssueTicket)
		v1.GET("/ws", handler.ServeWS)
	}
}
