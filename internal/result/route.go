package result

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts result routes.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	auth := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	// Authenticated routes
	protected := r.Group("")
	protected.Use(auth)
	{
		protected.GET("/results/:registration_id", h.GetResult)
		protected.POST("/rechecks", h.CreateRecheck)
	}

	// Admin-only result creation
	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		adminG.POST("/results", h.CreateResult)
	}
}
