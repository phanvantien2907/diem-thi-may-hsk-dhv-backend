package exam

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts exam routes.
//
//	public:  /exam-types, /exam-rooms, /exam-sessions, /exam-sessions/:id, /exam-sessions/:id/seats
//	admin:   /admin/exam-sessions/:id/seats/generate
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	// Public read endpoints
	r.GET("/exam-types", h.ListExamTypes)
	r.GET("/exam-rooms", h.ListExamRooms)
	r.GET("/exam-sessions", h.ListExamSessions)
	r.GET("/exam-sessions/:id", h.GetExamSession)
	r.GET("/exam-sessions/:id/seats", h.ListExamSeats)

	// Admin-only seat generation
	admin := r.Group("/admin/exam-sessions/:id/seats/generate")
	admin.Use(middleware.JWTAuth(jwtSecret))
	admin.Use(middleware.RequireRole("admin"))
	admin.POST("", h.GenerateSeats)
}
