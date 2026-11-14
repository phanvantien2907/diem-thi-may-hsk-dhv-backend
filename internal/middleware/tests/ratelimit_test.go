package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRateLimiter_ByAccountAndIP_And_CleanStop(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 2)
	defer rl.Stop()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(42))
		c.Next()
	})
	r.POST("/ocr", rl.RateLimitByAccountAndIP(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// First 2 requests within burst=2 succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/ocr", nil)
		req.RemoteAddr = "198.51.100.10:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected request %d to pass with 200, got %d", i+1, w.Code)
		}
	}

	// 3rd immediate request exceeds burst and returns 429 Too Many Requests
	req3 := httptest.NewRequest(http.MethodPost, "/ocr", nil)
	req3.RemoteAddr = "198.51.100.10:12345"
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 3rd request to be rate-limited with 429, got %d", w3.Code)
	}
}
