package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// PageLimit extracts pagination params from query string with sensible defaults.
func PageLimit(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}
