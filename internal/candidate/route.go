package candidate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all candidate routes on the given router group.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	h.SetJWTSecret(jwtSecret)
	auth := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	// ── Thí sinh (JWT required) ────────────────────────────────────────────
	me := r.Group("/me/candidate-profile")
	me.Use(auth)
	{
		me.POST("", h.CreateProfile)
		me.GET("", h.GetMyProfile)
		me.PATCH("", h.PatchMyProfile)
		me.POST("/documents", h.UploadDocument)
		me.GET("/documents", h.GetMyDocuments)
		me.GET("/upload-signature", h.GetUploadSignature)
		me.POST("/validate-portrait", h.ValidatePortrait)
		me.POST("/clear-cache", h.ClearMyCache)
	}

	// ── Admin (JWT + admin role required) ─────────────────────────────────
	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		// /admin/candidates/duplicates, merge, and clear-cache must be registered before /admin/candidates/:id
		// to prevent Gin from treating static segments as an :id param.
		adminG.GET("/candidates/duplicates", h.AdminListDuplicates)
		adminG.POST("/candidates/merge", h.AdminMergeCandidates)
		adminG.GET("/candidates/metrics", h.AdminCandidateMetrics)
		adminG.GET("/candidates/export", h.AdminExportCandidates)
		adminG.POST("/candidates/clear-cache", h.AdminClearCache)
		adminG.GET("/candidates", h.AdminListCandidates)
		adminG.GET("/candidates/:id", h.AdminGetCandidate)
		adminG.DELETE("/candidates/:id/lock", h.AdminReleaseCandidateLock)
		adminG.POST("/candidates/:id/approve", h.AdminApproveDocument)
		adminG.POST("/candidates/:id/reject", h.AdminRejectDocument)
		adminG.POST("/candidates/:id/revoke-approval", h.AdminRevokeApproval)
		adminG.POST("/candidates/:id/reopen-review", h.AdminReopenReview)
		adminG.GET("/exam-sessions/:id/roster", h.AdminGetRoster)
	}
}
