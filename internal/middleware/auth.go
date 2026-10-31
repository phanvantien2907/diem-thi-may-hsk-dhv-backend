package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hskvinh/backend/internal/cache"
	"github.com/hskvinh/backend/internal/exception"
)

const (
	// ContextKeyUserID is the gin.Context key used to store the authenticated user's ID.
	ContextKeyUserID = "user_id"
	// ContextKeyUserRole is the gin.Context key used to store the authenticated user's role.
	ContextKeyUserRole = "user_role"

	revokedAccountKeyPrefix = "hsk:auth:revoked_acct:"
)

var (
	revokedAccountsMu sync.RWMutex
	revokedAccounts   = make(map[int64]time.Time)
	authCacheSvc      *cache.Service
)

// SetAuthCache configures the shared Redis cache service for distributed JWT revocation checks.
func SetAuthCache(c *cache.Service) {
	revokedAccountsMu.Lock()
	authCacheSvc = c
	revokedAccountsMu.Unlock()
}

// RevokeAccountTokens immediately invalidates all active JWT access tokens for accountID
// across both in-memory and Redis stores for the given TTL.
func RevokeAccountTokens(ctx context.Context, accountID int64, ttl time.Duration) {
	if accountID <= 0 {
		return
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	exp := time.Now().UTC().Add(ttl)

	revokedAccountsMu.Lock()
	revokedAccounts[accountID] = exp
	c := authCacheSvc
	revokedAccountsMu.Unlock()

	if c != nil && c.Enabled() {
		_ = c.SetWithJitter(ctx, fmt.Sprintf("%s%d", revokedAccountKeyPrefix, accountID), exp, ttl, 0.01)
	}
}

// RestoreAccountTokens clears the JWT revocation marker for a reactivated account.
func RestoreAccountTokens(ctx context.Context, accountID int64) {
	if accountID <= 0 {
		return
	}
	revokedAccountsMu.Lock()
	delete(revokedAccounts, accountID)
	c := authCacheSvc
	revokedAccountsMu.Unlock()

	if c != nil && c.Enabled() {
		_ = c.Delete(ctx, fmt.Sprintf("%s%d", revokedAccountKeyPrefix, accountID))
	}
}

// IsAccountTokenRevoked checks whether accountID has been deactivated and its active JWTs revoked.
func IsAccountTokenRevoked(ctx context.Context, accountID int64) bool {
	if accountID <= 0 {
		return false
	}
	now := time.Now().UTC()

	revokedAccountsMu.RLock()
	exp, exists := revokedAccounts[accountID]
	c := authCacheSvc
	revokedAccountsMu.RUnlock()

	if exists {
		if exp.After(now) {
			return true
		}
		revokedAccountsMu.Lock()
		delete(revokedAccounts, accountID)
		revokedAccountsMu.Unlock()
	}

	if c != nil && c.Enabled() {
		var redisExp time.Time
		if hit, _ := c.Get(ctx, fmt.Sprintf("%s%d", revokedAccountKeyPrefix, accountID), &redisExp); hit && redisExp.After(now) {
			return true
		}
	}
	return false
}

// Claims defines the JWT payload structure.
type Claims struct {
	UserID   int64  `json:"user_id"`
	UserRole string `json:"user_role"`
	jwt.RegisteredClaims
}

// JWTAuth returns a Gin middleware that validates Bearer JWT tokens.
// The JWT secret is injected at construction time.
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			exception.Send(c, http.StatusUnauthorized, "authorization header is required")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		var tokenStr string
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") { // auto parse Bearer token from header
			tokenStr = parts[1]
		} else if len(parts) == 1 {
			// Fallback: accept token without prefix (convenience for Swagger UI)
			tokenStr = parts[0]
		} else {
			exception.Send(c, http.StatusUnauthorized, "authorization header must be in the format: Bearer <token>")
			return
		}

		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			exception.Send(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		if IsAccountTokenRevoked(c.Request.Context(), claims.UserID) {
			exception.Send(c, http.StatusUnauthorized, "tài khoản đã bị vô hiệu hóa hoặc phiên đăng nhập đã bị thu hồi")
			return
		}

		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyUserRole, claims.UserRole)
		c.Next()
	}
}

// RequireRole returns a Gin middleware that enforces one of the allowed roles.
// Must be used after JWTAuth.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(c *gin.Context) {
		role, _ := c.Get(ContextKeyUserRole)
		roleStr, _ := role.(string)

		if _, ok := allowed[roleStr]; !ok {
			exception.Send(c, http.StatusForbidden, "insufficient permissions")
			return
		}

		c.Next()
	}
}
