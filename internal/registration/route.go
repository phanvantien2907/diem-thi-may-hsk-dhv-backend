package registration

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts registration routes (all JWT-protected).
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	g := r.Group("/registrations")
	g.Use(middleware.JWTAuth(jwtSecret))
	{
		g.POST("", h.ReserveSeat)
		g.PUT("/:id/confirm", h.ConfirmRegistration)
		g.DELETE("/:id", h.CancelRegistration)
		g.GET("", h.GetMyRegistrations)
	}
}
