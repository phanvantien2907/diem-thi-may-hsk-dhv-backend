package location

import "github.com/gin-gonic/gin"

// RegisterRoutes mounts location routes (public, no auth required).
func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	r.GET("/provinces", h.ListProvinces)
	r.GET("/provinces/:id/wards", h.ListWards)
}
