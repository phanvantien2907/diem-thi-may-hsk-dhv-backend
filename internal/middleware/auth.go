package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	// ContextKeyUserID is the gin.Context key used to store the authenticated user's ID.
	ContextKeyUserID = "user_id"
	// ContextKeyUserRole is the gin.Context key used to store the authenticated user's role.
	ContextKeyUserRole = "user_role"
)

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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "authorization header is required",
				"data":    nil,
				"meta":    nil,
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "authorization header must be in the format: Bearer <token>",
				"data":    nil,
				"meta":    nil,
			})
			return
		}

		tokenStr := parts[1]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "invalid or expired token",
				"data":    nil,
				"meta":    nil,
			})
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
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "insufficient permissions",
				"data":    nil,
				"meta":    nil,
			})
			return
		}

		c.Next()
	}
}
