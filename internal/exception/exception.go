package exception

import (
	"time"

	"github.com/gin-gonic/gin"
)

// ExceptionResponse defines the standard error format for the API.
type ExceptionResponse struct {
	StatusCode int    `json:"statusCode"`
	Timestamp  string `json:"timestamp"`
	Path       string `json:"path"`
	Msg        string `json:"msg"`
}

func Send(c *gin.Context, code int, msg string) {
	resp := ExceptionResponse{
		StatusCode: code,
		Timestamp:  time.Now().In(time.FixedZone("VST", 7*3600)).Format(time.RFC3339),
		Path:       c.Request.URL.Path,
		Msg:        msg,
	}

	c.AbortWithStatusJSON(code, resp)
}
