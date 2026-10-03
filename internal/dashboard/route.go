package dashboard

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all dashboard admin routes.
//
//	admin (JWT + admin role):
//	  GET  /admin/metrics/overview                   — Top KPIs
//	  GET  /admin/metrics/revenue-chart              — Revenue line chart
//	  GET  /admin/metrics/hsk-distribution           — HSK pie chart
//	  GET  /admin/metrics/payment-funnel             — Peak-hour funnel
//	  GET  /admin/sessions/live-status               — Real-time session seats
//	  GET  /admin/transactions/exceptions            — Problematic transactions
//	  POST /admin/transactions/:id/manual-approve    — Force approve
//	  POST /admin/transactions/:id/refund-mark       — Mark for refund
//	  GET  /admin/logs/recent                        — Recent audit events
//	  POST /admin/system/clear-cache                 — Clear metrics cache
//	  GET  /admin/users/me/audit-log                 — Admin's own history
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		// Metrics & charts
		metrics := admin.Group("/metrics")
		{
			metrics.GET("/overview", h.GetMetricsOverview)
			metrics.GET("/revenue-chart", h.GetRevenueChart)
			metrics.GET("/hsk-distribution", h.GetHSKDistribution)
			metrics.GET("/payment-funnel", h.GetPaymentFunnel)
		}

		// Session monitoring
		admin.GET("/sessions/live-status", h.GetLiveSessionStatus)

		// Transaction exceptions
		txn := admin.Group("/transactions")
		{
			txn.GET("/exceptions", h.GetTransactionExceptions)
			txn.POST("/:id/manual-approve", h.ManualApprove)
			txn.POST("/:id/refund-mark", h.RefundMark)
		}

		// Logs & utilities
		admin.GET("/logs/recent", h.GetRecentLogs)
		admin.POST("/system/clear-cache", h.ClearCache)
		admin.GET("/users/me/audit-log", h.GetMyAuditLog)
	}
}
