package certificate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all certificate routes.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	auth := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	// ── Me (candidate) ─────────────────────────────────────────────────────
	me := r.Group("/me")
	me.Use(auth)
	{
		me.GET("/certificates", h.ListMyCertificates)
		me.GET("/certificates/:id", h.GetMyCertificate)
		me.GET("/certificates/:id/pdf", h.DownloadMyCertificatePDF)
		me.POST("/certificates/:id/deliveries", h.CreateDelivery)
		me.GET("/certificates/:id/deliveries", h.GetMyDelivery)
		me.POST("/certificates/:id/reissue-request", h.ReissueRequest)
		me.PATCH("/deliveries/:delivery_id", h.UpdateMyDeliveryAddress)
	}

	// ── Public (no auth) ───────────────────────────────────────────────────
	public := r.Group("/public")
	{
		public.GET("/certificates/verify", h.VerifyCertificate)
	}

	// ── Admin ──────────────────────────────────────────────────────────────
	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		adminG.POST("/certificates/issue-batch", h.IssueBatch)
		adminG.POST("/certificates/:id/issue", h.IssueCertificate)
		adminG.POST("/certificates/:id/revoke", h.RevokeCertificate)
		adminG.GET("/certificates", h.ListCertificates)
		adminG.GET("/certificates/print-batch", h.PrintBatch)
		adminG.GET("/deliveries", h.ListDeliveries)
		adminG.PATCH("/deliveries/:id", h.AdminUpdateDelivery)
	}

	// ── Webhooks (HMAC-verified, no JWT) ───────────────────────────────────
	webhooks := r.Group("/webhooks")
	{
		webhooks.POST("/shipping/:courier", h.ShippingWebhook)
	}
}
