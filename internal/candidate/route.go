package candidate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts candidate routes (all JWT-protected).
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	g := r.Group("/candidates")
	g.Use(middleware.JWTAuth(jwtSecret))
	{
		g.POST("", h.CreateProfile)
		g.PUT("/:id", h.UpdateProfile)
		g.POST("/:id/documents", h.UploadDocument)
	}
}
