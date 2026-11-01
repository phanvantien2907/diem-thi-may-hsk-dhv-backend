package registration

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all registration-related routes.
//
//	Candidate (/me/):
//	  POST   /me/registrations                              — đặt ghế
//	  GET    /me/registrations                              — danh sách đăng ký
//	  POST   /me/registrations/:id/payment-confirm          — xác nhận thanh toán
//	  POST   /me/registrations/:id/transfer                 — chuyển ca thi
//	  POST   /me/registrations/:id/cancel                   — hủy đăng ký
//	  GET    /me/registrations/:id/admission-slip           — phiếu dự thi
//	  POST   /me/exam-sessions/:id/waitlist                 — vào danh sách chờ
//
//	Proctor:
//	  PATCH  /proctor/exam-sessions/:id/attendance/:registration_id
//
//	Admin:
//	  GET    /admin/exam-sessions/:id/dashboard
//	  GET    /admin/payments/reconcile
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	// ── Candidate routes ──────────────────────────────────────────────────
	me := r.Group("/me")
	me.Use(middleware.JWTAuth(jwtSecret))
	{
		reg := me.Group("/registrations")
		reg.POST("", h.ReserveSeat)
		reg.GET("", h.GetMyRegistrations)
		reg.POST("/:id/payment-confirm", h.PaymentConfirm)
		reg.POST("/:id/transfer", h.TransferRegistration)
		reg.POST("/:id/cancel", h.CancelRegistration)
		reg.GET("/:id/admission-slip", h.GetAdmissionSlip)

		me.POST("/exam-sessions/:id/waitlist", h.JoinWaitlist)
	}

	// ── Proctor routes (JWT only — no special role check yet) ─────────────
	proctor := r.Group("/proctor")
	proctor.Use(middleware.JWTAuth(jwtSecret))
	{
		proctor.PATCH("/exam-sessions/:id/attendance/:registration_id", h.MarkAttendance)
	}

	// ── Admin routes ──────────────────────────────────────────────────────
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		admin.GET("/exam-sessions/:id/dashboard", h.GetSessionDashboard)
		admin.GET("/payments/reconcile", h.GetPaymentReconcile)

		// Registration Results module (Kết quả Đăng ký theo Đợt thi & Ca thi)
		results := admin.Group("/registration-results")
		{
			results.GET("/periods", h.ListPeriodRegistrationResults)
			results.GET("/periods/:period_id", h.GetPeriodRegistrationResults)
			results.GET("/sessions/:session_id/summary", h.GetSlotRegistrationSummary)
			results.GET("/sessions/:session_id/registrations", h.ListSlotRegistrations)
			results.GET("/sessions/:session_id/waitlist", h.AdminListWaitlist)
			results.GET("/discrepancies", h.AdminCheckDiscrepancies)
			results.GET("/export", h.AdminExportRegistrations)
		}

		// Admin registration lifecycle actions (cancel + refund workflow, slot transfer)
		admin.POST("/registrations/:id/cancel", h.AdminCancelRegistration)
		admin.POST("/registrations/:id/transfer", h.AdminTransferRegistration)
	}
}
