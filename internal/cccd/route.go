package cccd

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts the CCCD OCR endpoints under /api/v1/cccd.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	group := r.Group("/cccd")
	group.Use(middleware.JWTAuth(jwtSecret))
	{
		group.POST("/upload", h.Upload)
		group.POST("/extract", h.Extract)
		group.GET("/jobs/:id", h.GetJobStatus)
	}
}
