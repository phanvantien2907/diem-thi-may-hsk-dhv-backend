package testutils

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetupTestRouter creates a test Gin engine and returns a context and recorder for testing single handlers.
func SetupTestRouter() (*gin.Context, *httptest.ResponseRecorder, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	return c, w, r
}

// BuildJSONRequest creates an HTTP request with a JSON body.
func BuildJSONRequest(method, url string, body any) *http.Request {
	var bodyReader io.Reader
	if body != nil {
		if str, ok := body.(string); ok {
			bodyReader = strings.NewReader(str)
		} else {
			b, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(b)
		}
	}
	req := httptest.NewRequest(method, url, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	return req
}
