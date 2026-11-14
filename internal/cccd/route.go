package cccd

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts the CCCD OCR endpoints under /api/v1/cccd with strict per-account and per-IP rate limiting.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	ocrLimiter := middleware.NewRateLimiter(2, 5) // 2 RPS, burst 5 per account and per IP
	group := r.Group("/cccd")
	group.Use(middleware.JWTAuth(jwtSecret))
	{
		group.POST("/upload", ocrLimiter.RateLimitByAccountAndIP(), h.Upload)
		group.POST("/extract", ocrLimiter.RateLimitByAccountAndIP(), h.Extract)
		group.GET("/jobs/:id", h.GetJobStatus)
	}
}
