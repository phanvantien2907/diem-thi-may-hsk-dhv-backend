package candidate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all candidate routes on the given router group.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string) {
	auth  := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	// ── Thí sinh (JWT required) ────────────────────────────────────────────
	me := r.Group("/me/candidate-profile")
	me.Use(auth)
	{
		me.POST("", h.CreateProfile)
		me.GET("", h.GetMyProfile)
		me.PATCH("", h.PatchMyProfile)
		me.POST("/documents", h.UploadDocument)
		me.GET("/upload-signature", h.GetUploadSignature)
	}

	// ── Admin (JWT + admin role required) ─────────────────────────────────
	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		// /admin/candidates/duplicates must be registered before /admin/candidates/:id
		// to prevent Gin from treating "duplicates" as an :id param.
		adminG.GET("/candidates/duplicates", h.AdminListDuplicates)
		adminG.GET("/candidates", h.AdminListCandidates)
		adminG.GET("/candidates/:id", h.AdminGetCandidate)
		adminG.POST("/candidates/:id/approve", h.AdminApproveDocument)
		adminG.POST("/candidates/:id/reject", h.AdminRejectDocument)
		adminG.GET("/exam-sessions/:id/roster", h.AdminGetRoster)
	}
}
