package payment

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts payment routes.
//
//	public (webhook):
//	  POST /payments/callback                    — PayOS webhook (authenticated via HMAC signature)
//
//	candidate (JWT):
//	  POST /payments/payos/create                — Create PayOS checkout link
//	  GET  /payments?candidate_id=X              — Payment history
//
//	admin (JWT + admin role):
//	  GET  /admin/payments                       — List all payments (paginated)
//	  POST /admin/payments/:id/confirm           — Manual confirm
//	  POST /admin/payments/:id/cancel            — Cancel / refund
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	g := r.Group("/payments")

	// Webhook callback — no JWT, authenticated via PayOS Checksum.
	g.POST("/callback", h.Callback)

	// Protected candidate routes.
	protected := g.Group("")
	protected.Use(middleware.JWTAuth(jwtSecret))
	{
		protected.POST("/payos/create", h.CreatePayOSLink)
		protected.GET("", h.GetPaymentHistory)
	}

	// Admin-only routes.
	admin := r.Group("/admin")
	admin.Use(middleware.JWTAuth(jwtSecret), middleware.RequireRole("admin"))
	{
		admin.GET("/payments", h.AdminListPayments)
		admin.POST("/payments/:id/confirm", h.AdminConfirmPayment)
		admin.POST("/payments/:id/cancel", h.AdminCancelPayment)
	}
}
