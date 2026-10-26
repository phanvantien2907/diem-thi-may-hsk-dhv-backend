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
//	  POST /exam-sessions/clear-cache
//
//	admin — exam periods:
//	  GET    /admin/exam-periods
//	  POST   /admin/exam-periods
//	  GET    /admin/exam-periods/:id
//	  PUT    /admin/exam-periods/:id
//	  DELETE /admin/exam-periods/:id              (draft only)
//	  POST   /admin/exam-periods/:id/duplicate
//	  POST   /admin/exam-periods/:id/publish
//	  POST   /admin/exam-periods/:id/unpublish
//	  POST   /admin/exam-periods/:id/close-registration
//	  POST   /admin/exam-periods/:id/reopen-registration
//	  POST   /admin/exam-periods/:id/reschedule
//	  POST   /admin/exam-periods/:id/complete
//	  POST   /admin/exam-periods/:id/cancel
//
//	admin — exam sessions:
//	  GET    /admin/exam-sessions
//	  POST   /admin/exam-sessions/batch-create
//	  POST   /admin/exam-sessions/:id/seats/generate
//	  POST   /admin/exam-sessions/:id/publish
//	  POST   /admin/exam-sessions/:id/schedule-publish
//	  POST   /admin/exam-sessions/:id/unpublish
//	  POST   /admin/exam-sessions/:id/close-registration
//	  POST   /admin/exam-sessions/:id/reopen-registration
//	  POST   /admin/exam-sessions/:id/capacity
//	  POST   /admin/exam-sessions/:id/duplicate
//	  POST   /admin/exam-sessions/:id/cancel
//	  DELETE /admin/exam-sessions/:id/draft
//	  PUT    /admin/exam-sessions/:id
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
		// Exam periods
		admin.GET("/exam-periods", h.AdminListExamPeriods)
		admin.POST("/exam-periods", h.AdminCreateExamPeriod)
		admin.GET("/exam-periods/:id", h.AdminGetExamPeriod)
		admin.PUT("/exam-periods/:id", h.AdminUpdateExamPeriod)
		admin.DELETE("/exam-periods/:id", h.AdminDeleteExamPeriod)
		admin.POST("/exam-periods/:id/duplicate", h.AdminDuplicateExamPeriod)
		admin.POST("/exam-periods/:id/publish", h.AdminPublishExamPeriod)
		admin.POST("/exam-periods/:id/unpublish", h.AdminUnpublishExamPeriod)
		admin.POST("/exam-periods/:id/close-registration", h.AdminClosePeriodRegistration)
		admin.POST("/exam-periods/:id/reopen-registration", h.AdminReopenPeriodRegistration)
		admin.POST("/exam-periods/:id/reschedule", h.AdminRescheduleExamPeriod)
		admin.POST("/exam-periods/:id/complete", h.AdminCompleteExamPeriod)
		admin.POST("/exam-periods/:id/cancel", h.AdminCancelExamPeriod)

		// batch-create and clear-cache must be registered BEFORE /:id routes to avoid conflict
		admin.GET("/exam-sessions", h.ListAdminExamSessions)
		admin.POST("/exam-sessions/clear-cache", h.ClearCache)
		admin.POST("/exam-sessions/batch-create", h.BatchCreateExamSessions)
		admin.POST("/exam-sessions/:id/seats/generate", h.GenerateSeats)
		admin.POST("/exam-sessions/:id/publish", h.PublishExamSession)
		admin.POST("/exam-sessions/:id/schedule-publish", h.ScheduleSessionPublish)
		admin.POST("/exam-sessions/:id/unpublish", h.UnpublishExamSession)
		admin.POST("/exam-sessions/:id/close-registration", h.AdminCloseSessionRegistration)
		admin.POST("/exam-sessions/:id/reopen-registration", h.AdminReopenSessionRegistration)
		admin.POST("/exam-sessions/:id/capacity", h.UpdateSessionCapacity)
		admin.POST("/exam-sessions/:id/duplicate", h.AdminDuplicateExamSession)
		admin.POST("/exam-sessions/:id/cancel", h.CancelExamSession)
		admin.DELETE("/exam-sessions/:id/draft", h.DeleteExamSessionDraft)
		admin.PUT("/exam-sessions/:id", h.AdminUpdateExamSession)
	}
}
