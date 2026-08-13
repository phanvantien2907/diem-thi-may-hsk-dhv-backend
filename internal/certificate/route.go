package certificate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts certificate routes.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	auth := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	protected := r.Group("")
	protected.Use(auth)
	{
		protected.GET("/certificates/:result_id", h.GetCertificate)
		protected.POST("/certificates/:id/deliveries", h.CreateDelivery)
	}

	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		adminG.POST("/certificates", h.IssueCertificate)
	}
}
