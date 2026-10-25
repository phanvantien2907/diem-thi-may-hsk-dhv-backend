package exam

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts exam routes.
//
//	public:
//	  GET  /exam-types
//	  GET  /exam-sessions                   (filter: exam_type_id, from, to)
//	  GET  /exam-sessions/:id
//	  GET  /exam-sessions/:id/seats
//
//	admin:
//	  GET  /admin/exam-sessions                   (filter: exam_type_id, from, to)
//	  POST /admin/exam-sessions/batch-create
//	  POST /admin/exam-sessions/:id/seats/generate
//	  POST /admin/exam-sessions/:id/cancel
//	  PATCH /admin/exam-sessions/:id/publication-status
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	// Public read endpoints
	r.GET("/exam-types", h.ListExamTypes)
	r.GET("/exam-sessions", h.ListExamSessions)
	r.GET("/exam-sessions/:id", h.GetExamSession)
	r.GET("/exam-sessions/:id/seats", h.ListExamSeats)
	r.POST("/exam-sessions/clear-cache", h.ClearCache)

	// Admin-only
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		// Exam periods management
		admin.GET("/exam-periods", h.AdminListExamPeriods)
		admin.POST("/exam-periods", h.AdminCreateExamPeriod)
		admin.GET("/exam-periods/:id", h.AdminGetExamPeriod)
		admin.PUT("/exam-periods/:id", h.AdminUpdateExamPeriod)
		admin.POST("/exam-periods/:id/duplicate", h.AdminDuplicateExamPeriod)
		admin.POST("/exam-periods/:id/reschedule", h.AdminRescheduleExamPeriod)
		admin.POST("/exam-periods/:id/close-registration", h.AdminClosePeriodRegistration)

		// batch-create and clear-cache must be registered BEFORE /:id routes to avoid conflict
		admin.GET("/exam-sessions", h.ListAdminExamSessions)
		admin.POST("/exam-sessions/clear-cache", h.ClearCache)
		admin.POST("/exam-sessions/batch-create", h.BatchCreateExamSessions)
		admin.POST("/exam-sessions/:id/seats/generate", h.GenerateSeats)
		admin.POST("/exam-sessions/:id/cancel", h.CancelExamSession)
		admin.POST("/exam-sessions/:id/close-registration", h.AdminCloseSessionRegistration)
		admin.POST("/exam-sessions/:id/duplicate", h.AdminDuplicateExamSession)
		admin.PATCH("/exam-sessions/:id/publication-status", h.UpdatePublicationStatus)
		admin.PUT("/exam-sessions/:id", h.AdminUpdateExamSession)
	}
}
