package utils

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

// MustAccountID get account ID from context.
func MustAccountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}
