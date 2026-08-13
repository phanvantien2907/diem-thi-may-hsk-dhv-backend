package payment

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts payment routes (all JWT-protected).
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	g := r.Group("/payments")
	g.Use(middleware.JWTAuth(jwtSecret))
	{
		g.POST("", h.CreatePayment)
		g.POST("/callback", h.Callback)
		g.GET("", h.GetPaymentHistory)
	}
}
