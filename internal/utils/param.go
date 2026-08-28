package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// Parse string to int from params.
func ParamID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
