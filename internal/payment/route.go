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
		// Evidence & helper endpoints
		admin.GET("/payments/evidence-upload-signature", h.GetEvidenceUploadSignature)

		// Edge case: Double charge
		admin.GET("/payments/double-charges", h.AdminGetDoubleCharges)
		admin.POST("/payments/resolve-double-charge", h.AdminResolveDoubleCharge)

		// Edge case: Suspense account / unmatched bank transfers
		admin.GET("/payments/suspense", h.AdminListSuspense)
		admin.POST("/payments/suspense", h.AdminCreateSuspense)
		admin.POST("/payments/suspense/import", h.AdminImportSuspense)
		admin.POST("/payments/suspense/:id/match", h.AdminMatchSuspense)

		// List & export
		admin.GET("/payments", h.AdminListPayments)
		admin.GET("/payments/export", h.AdminExportPayments)

		// Single payment actions & edge cases
		admin.POST("/payments/:id/confirm", h.AdminConfirmPayment)
		admin.POST("/payments/:id/sync", h.AdminSyncPayment)
		admin.POST("/payments/:id/cancel", h.AdminCancelPayment)
		admin.POST("/payments/:id/mark-refund", h.AdminMarkRefund)
		admin.POST("/payments/:id/confirm-refund", h.AdminConfirmRefund)
		admin.POST("/payments/:id/top-up", h.AdminTopUpPayment)
		admin.POST("/payments/:id/resolve-overpayment", h.AdminResolveOverpayment)
	}
}
