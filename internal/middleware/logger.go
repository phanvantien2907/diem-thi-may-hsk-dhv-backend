package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger returns a Gin middleware that logs each request's method, path,
// status code, latency, and client IP.
func Logger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		// Format: [GIN] 2006/01/02 - 15:04:05 | 200 | 1.23ms | 127.0.0.1 | GET /api/v1/health
		return param.TimeStamp.Format(time.DateTime) +
			" | " + param.ClientIP +
			" | " + param.Method +
			" " + param.Path +
			" | " + param.StatusCodeColor() + fmt.Sprintf("%d", param.StatusCode) + param.ResetColor() +
			" | " + param.Latency.String() +
			"\n"
	})
}
