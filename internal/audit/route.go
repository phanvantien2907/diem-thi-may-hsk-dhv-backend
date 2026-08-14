package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts audit routes on the given router group.
//
//	admin: GET /admin/audit-logs  (JWT + role=admin required)
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		admin.GET("/audit-logs", h.List)
	}
}
