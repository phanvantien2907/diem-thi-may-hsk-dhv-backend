package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts auth routes on the given router group.
//
//	public:      /auth/register, /auth/login
//	protected:   /me (GET, PUT, DELETE)
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	r.POST("/auth/register", h.Register)
	r.POST("/auth/login", h.Login)

	me := r.Group("/me")
	me.Use(middleware.JWTAuth(jwtSecret))
	{
		me.GET("", h.Me)
		me.PUT("", h.UpdateMe)
		me.DELETE("", h.DeleteMe)
	}

	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		admin.DELETE("/delete-account/:id", h.AdminDeleteAccount)
	}
}
