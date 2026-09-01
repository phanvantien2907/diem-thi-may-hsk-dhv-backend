package exam

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts exam routes.
//
//	public:
//	  GET  /exam-types
//	  GET  /exam-rooms
//	  GET  /exam-sessions                   (filter: exam_type_id, from, to)
//	  GET  /exam-sessions/:id
//	  GET  /exam-sessions/:id/seats
//
//	admin:
//	  POST /admin/exam-sessions/batch-create
//	  POST /admin/exam-sessions/:id/seats/generate
//	  POST /admin/exam-sessions/:id/cancel
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	// Public read endpoints
	r.GET("/exam-types", h.ListExamTypes)
	r.GET("/exam-rooms", h.ListExamRooms)
	r.GET("/exam-sessions", h.ListExamSessions)
	r.GET("/exam-sessions/:id", h.GetExamSession)
	r.GET("/exam-sessions/:id/seats", h.ListExamSeats)

	// Admin-only
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		// batch-create must be registered BEFORE /:id routes to avoid conflict
		admin.POST("/exam-sessions/batch-create", h.BatchCreateExamSessions)
		admin.POST("/exam-sessions/:id/seats/generate", h.GenerateSeats)
		admin.POST("/exam-sessions/:id/cancel", h.CancelExamSession)
	}
}
