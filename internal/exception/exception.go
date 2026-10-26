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
	Code       string `json:"code,omitempty"`
	Msg        string `json:"msg"`
}

func Send(c *gin.Context, code int, msg string) {
	send(c, code, "", msg)
}

// SendWithCode sends an error response carrying a stable machine-readable code.
func SendWithCode(c *gin.Context, statusCode int, code, msg string) {
	send(c, statusCode, code, msg)
}

func send(c *gin.Context, statusCode int, code, msg string) {
	resp := ExceptionResponse{
		StatusCode: statusCode,
		Timestamp:  time.Now().In(time.FixedZone("VST", 7*3600)).Format(time.RFC3339),
		Path:       c.Request.URL.Path,
		Code:       code,
		Msg:        msg,
	}

	c.AbortWithStatusJSON(statusCode, resp)
}
