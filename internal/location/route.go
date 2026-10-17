package location

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts location routes.
// Public routes require no auth. Admin routes require JWT + admin role.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	// ── Public ─────────────────────────────────────────────────────────────
	loc := r.Group("/locations")
	{
		loc.GET("/provinces", h.ListProvinces)
		loc.GET("/provinces/:id/wards", h.ListWards)
		loc.GET("/wards/:id/delivery-coverage", h.GetWardDeliveryCoverage)
		loc.POST("/clear-cache", h.ClearCache)
	}

	// ── Admin (JWT + admin role required) ──────────────────────────────────
	admin := r.Group("/admin/locations")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		admin.GET("/stats/candidates-by-province", h.StatsCandidatesByProvince)
		admin.POST("/wards/:id/merge", h.MergeWard)
		admin.POST("/clear-cache", h.ClearCache)
	}
}
