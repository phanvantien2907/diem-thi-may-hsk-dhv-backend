package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// RegisterRoutes mounts all auth routes on the given router group.
func RegisterRoutes(r *gin.RouterGroup, h *Handler, jwtSecret string, authLimiter *middleware.RateLimiter) {
	auth := middleware.JWTAuth(jwtSecret)
	admin := middleware.RequireRole("admin")

	// ── Public (no auth) ───────────────────────────────────────────────────
	publicAuth := r.Group("/auth")
	if authLimiter != nil {
		publicAuth.Use(authLimiter.RateLimit())
	}
	{
		publicAuth.POST("/register", h.Register)
		publicAuth.POST("/login", h.Login)
		publicAuth.POST("/refresh", h.RefreshToken)
		publicAuth.POST("/force-password-change", h.ForceChangePassword)
	}

	// ── Authenticated ─────────────────────────────────────────────────────
	protected := r.Group("")
	protected.Use(auth)
	{
		// Logout
		protected.POST("/auth/logout", h.Logout)
		protected.POST("/auth/logout-all", h.LogoutAll)

		// Me
		protected.GET("/me", h.Me)
		protected.PUT("/me", h.UpdateMe)
		protected.DELETE("/me", h.DeleteMe)
		protected.PATCH("/me/password", h.ChangePassword)
		protected.GET("/me/sessions", h.ListSessions)
		protected.DELETE("/me/sessions/:id", h.DeleteSession)
	}

	// ── Admin ──────────────────────────────────────────────────────────────
	adminG := r.Group("/admin")
	adminG.Use(auth, admin)
	{
		adminG.DELETE("/delete-account/:id", h.AdminDeleteAccount)
		adminG.POST("/accounts/:id/deactivate", h.AdminDeleteAccount)
		adminG.POST("/accounts/:id/reactivate", h.AdminReactivateAccount)
		adminG.POST("/staff-accounts", h.CreateStaffAccount)
	}
}
