package notification

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts notification routes (all JWT-protected).
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	g := r.Group("/notifications")
	g.Use(middleware.JWTAuth(jwtSecret))
	{
		g.GET("", h.GetMyNotifications)
		g.PATCH("/:id/read", h.MarkRead)
		g.PUT("/:id/read", h.MarkRead)
	}
}
