package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// visitor tracks the token bucket state for a single IP address.
type visitor struct {
	tokens   float64
	lastSeen time.Time
	mu       sync.Mutex
}

// RateLimiter is a simple in-memory token bucket rate limiter keyed by client IP.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rps      float64 // refill rate: tokens per second
	burst    float64 // bucket capacity
}

// NewRateLimiter creates a new RateLimiter and starts a background cleanup goroutine.
func NewRateLimiter(rps, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rps:      float64(rps),
		burst:    float64(burst),
	}
	go rl.cleanupLoop()
	return rl
}

// allow returns true when the request from the given IP is within the rate limit.
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	v, exists := rl.visitors[ip]
	if !exists {
		v = &visitor{tokens: rl.burst, lastSeen: time.Now()}
		rl.visitors[ip] = v
	}
	rl.mu.Unlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(v.lastSeen).Seconds()
	v.lastSeen = now

	// Refill tokens based on elapsed time.
	v.tokens += elapsed * rl.rps
	if v.tokens > rl.burst {
		v.tokens = rl.burst
	}

	if v.tokens < 1 {
		return false
	}

	v.tokens--
	return true
}

// cleanupLoop removes stale IP entries every 5 minutes.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			v.mu.Lock()
			if time.Since(v.lastSeen) > 10*time.Minute {
				delete(rl.visitors, ip)
			}
			v.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// RateLimit returns a Gin middleware that enforces the per-IP rate limit.
func (rl *RateLimiter) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !rl.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error":   "too many requests, please slow down",
				"data":    nil,
				"meta":    nil,
			})
			return
		}
		c.Next()
	}
}

// RateLimitByAccountAndIP enforces rate limiting per client IP AND per authenticated account ID
// (when ContextKeyUserID is present) to protect expensive upstream APIs such as Gemini Vision OCR.
func (rl *RateLimiter) RateLimitByAccountAndIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		ipKey := "ip:" + c.ClientIP()
		if !rl.allow(ipKey) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error":   "too many requests from this IP, please slow down",
				"data":    nil,
				"meta":    nil,
			})
			return
		}
		if uidVal, exists := c.Get(ContextKeyUserID); exists {
			if uid, ok := uidVal.(int64); ok && uid > 0 {
				acctKey := fmt.Sprintf("acct:%d", uid)
				if !rl.allow(acctKey) {
					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"success": false,
						"error":   "too many OCR requests for this account, please wait a moment",
						"data":    nil,
						"meta":    nil,
					})
					return
				}
			}
		}
		c.Next()
	}
}
