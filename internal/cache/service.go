package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Sentinel value used to cache non-existent entities and prevent Cache Penetration.
const sentinelNotFound = "__HSK_NOT_FOUND__"

// ErrCacheMiss indicates that the requested key was not found in cache.
var ErrCacheMiss = errors.New("cache: key not found")

// ErrSentinelNotFound indicates that a sentinel for non-existent record was hit.
var ErrSentinelNotFound = errors.New("cache: record does not exist")

// Options configures the Redis cache connection.
type Options struct {
	Addr     string
	Password string
	DB       int
}

// Service provides a resilient, defensive Cache-Aside client.
type Service struct {
	client  *redis.Client
	sf      singleflight.Group
	rngMu   sync.Mutex
	rng     *rand.Rand
	enabled bool
}

// NewService initializes the Redis client and checks connectivity.
// If Redis is unreachable, it logs a warning and enters graceful degradation mode
// (all cache calls safely fall back to database queries without failing).
func NewService(opts Options) *Service {
	s := &Service{
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		enabled: false,
	}

	if opts.Addr == "" {
		log.Println("[cache] REDIS_ADDR empty, running without Redis cache")
		return s
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	s.client = rdb
	s.enabled = true

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[cache] warning: Redis ping failed (%v). Running in graceful fallback mode (DB direct)", err)
		// We still keep the client so it can automatically reconnect when Redis becomes healthy
	} else {
		log.Printf("[cache] connected to Redis at %s (db=%d)", opts.Addr, opts.DB)
	}

	return s
}

// Enabled returns true if the Redis cache is configured and enabled.
func (s *Service) Enabled() bool {
	return s != nil && s.enabled && s.client != nil
}

// jitter adds random jitter to TTL to prevent Cache Avalanche.
// If jitterPercent <= 0, defaults to 0.10 (±10%).
func (s *Service) jitter(ttl time.Duration, jitterPercent float64) time.Duration {
	if ttl <= 0 {
		return ttl
	}
	if jitterPercent <= 0 {
		jitterPercent = 0.10
	}
	s.rngMu.Lock()
	// factor ranges from (1 - jitterPercent) to (1 + jitterPercent)
	factor := (1.0 - jitterPercent) + s.rng.Float64()*(2.0*jitterPercent)
	s.rngMu.Unlock()
	return time.Duration(float64(ttl) * factor)
}

func (s *Service) rngInt63() int64 {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return s.rng.Int63()
}

// luaReleaseLock atomically releases the lock only if the value matches the token
const luaReleaseLock = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`

// AcquireLock attempts to acquire a distributed lock via SET NX PX.
// Returns (token, true, nil) if acquired, ("", false, nil) if already locked or Redis disabled.
func (s *Service) AcquireLock(ctx context.Context, lockKey string, ttl time.Duration) (string, bool, error) {
	if s == nil || !s.enabled || s.client == nil {
		return "", false, nil
	}
	token := fmt.Sprintf("%d-%d", time.Now().UnixNano(), s.rngInt63())
	ok, err := s.client.SetNX(ctx, lockKey, token, ttl).Result()
	if err != nil {
		log.Printf("[cache] acquire lock error for %s: %v", lockKey, err)
		return "", false, nil // Graceful degradation
	}
	if !ok {
		return "", false, nil
	}
	return token, true, nil
}

// ReleaseLock releases the distributed lock if the token matches.
func (s *Service) ReleaseLock(ctx context.Context, lockKey string, token string) error {
	if s == nil || !s.enabled || s.client == nil || token == "" {
		return nil
	}
	return s.client.Eval(ctx, luaReleaseLock, []string{lockKey}, token).Err()
}

// Get retrieves a key and deserializes it into dest.
// Returns (true, nil) on hit, (false, nil) on miss/Redis error, or (false, ErrSentinelNotFound).
func (s *Service) Get(ctx context.Context, key string, dest any) (bool, error) {
	if s == nil || !s.enabled || s.client == nil {
		return false, nil
	}

	val, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		// Graceful degradation: Log warning and treat as cache miss
		log.Printf("[cache] get error for key=%s: %v", key, err)
		return false, nil
	}

	if string(val) == sentinelNotFound {
		return false, ErrSentinelNotFound
	}

	if err := json.Unmarshal(val, dest); err != nil {
		log.Printf("[cache] unmarshal error for key=%s: %v", key, err)
		return false, nil
	}

	return true, nil
}

// Set stores a key-value pair in Redis with serialized JSON and default ±10% jitter.
func (s *Service) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	return s.SetWithJitter(ctx, key, val, ttl, 0.10)
}

// SetWithJitter stores a key-value pair with a customized jitter percentage (e.g. 0.35 for ±35%).
func (s *Service) SetWithJitter(ctx context.Context, key string, val any, ttl time.Duration, jitterPercent float64) error {
	if s == nil || !s.enabled || s.client == nil {
		return nil
	}

	bytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("cache: marshal: %w", err)
	}

	jittered := s.jitter(ttl, jitterPercent)
	if err := s.client.Set(ctx, key, bytes, jittered).Err(); err != nil {
		log.Printf("[cache] set error for key=%s: %v", key, err)
		return nil // Non-fatal for application
	}
	return nil
}

// SetSentinel caches an empty sentinel for non-existent entities to prevent Cache Penetration.
// WARNING: Do NOT use this on user-owned endpoints (/me/*). Only use on public lookups with random IDs.
func (s *Service) SetSentinel(ctx context.Context, key string, ttl time.Duration) {
	if s == nil || !s.enabled || s.client == nil {
		return
	}
	_ = s.client.Set(ctx, key, sentinelNotFound, ttl).Err()
}

// Delete removes one or more keys from Redis.
func (s *Service) Delete(ctx context.Context, keys ...string) error {
	if s == nil || !s.enabled || s.client == nil || len(keys) == 0 {
		return nil
	}
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		log.Printf("[cache] delete error for keys=%v: %v", keys, err)
	}
	return nil
}

// DeletePrefix deletes all keys matching a prefix using non-blocking SCAN.
func (s *Service) DeletePrefix(ctx context.Context, prefix string) error {
	if s == nil || !s.enabled || s.client == nil {
		return nil
	}

	match := prefix
	if match[len(match)-1] != '*' {
		match += "*"
	}

	iter := s.client.Scan(ctx, 0, match, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= 100 {
			_ = s.client.Del(ctx, keys...).Err()
			keys = keys[:0]
		}
	}
	if len(keys) > 0 {
		_ = s.client.Del(ctx, keys...).Err()
	}
	if err := iter.Err(); err != nil {
		log.Printf("[cache] delete prefix scan error for prefix=%s: %v", prefix, err)
	}
	return nil
}

// Remember executes Cache-Aside with Singleflight and Distributed Lock concurrency protection.
func (s *Service) Remember(ctx context.Context, key string, ttl time.Duration, dest any, fn func() (any, error)) error {
	return s.RememberWithJitter(ctx, key, ttl, 0.10, dest, fn)
}

// RememberWithJitter executes Cache-Aside with:
// 1. Singleflight (deduplicating concurrent requests on this instance)
// 2. Redis Distributed Lock (SET NX) to deduplicate multi-instance cold start across N instances
// 3. Dynamic TTL Jitter to prevent stampede expiration
func (s *Service) RememberWithJitter(ctx context.Context, key string, ttl time.Duration, jitterPercent float64, dest any, fn func() (any, error)) error {
	hit, err := s.Get(ctx, key, dest)
	if err != nil {
		if errors.Is(err, ErrSentinelNotFound) {
			return ErrSentinelNotFound
		}
	}
	if hit {
		return nil
	}

	// Singleflight protects against in-process Cache Stampede / Breakdown
	v, err, _ := s.sf.Do(key, func() (any, error) {
		// Double check inside singleflight
		var innerDest any
		if innerHit, _ := s.Get(ctx, key, &innerDest); innerHit {
			return innerDest, nil
		}

		// Distributed Lock (SET NX) protects multi-instance cold start across N instances
		lockKey := "lock:" + key
		token, acquired, _ := s.AcquireLock(ctx, lockKey, 5*time.Second)
		if acquired {
			defer func() {
				_ = s.ReleaseLock(ctx, lockKey, token)
			}()

			// Double check if another instance populated cache right before lock was acquired
			if innerHit, _ := s.Get(ctx, key, &innerDest); innerHit {
				return innerDest, nil
			}

			// Single instance computes from Postgres
			res, err := fn()
			if err != nil {
				return nil, err
			}

			// Store in cache with dynamic jitter
			_ = s.SetWithJitter(ctx, key, res, ttl, jitterPercent)
			return res, nil
		}

		// Lock was not acquired: another instance is recomputing.
		// Wait and poll briefly (up to 1s with 50ms intervals) to see if cache becomes available.
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(1 * time.Second)

		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timeout:
				// Fallback: If wait timed out or Redis is down, safely compute directly
				res, err := fn()
				if err != nil {
					return nil, err
				}
				_ = s.SetWithJitter(ctx, key, res, ttl, jitterPercent)
				return res, nil
			case <-ticker.C:
				var polled any
				if pollHit, _ := s.Get(ctx, key, &polled); pollHit {
					return polled, nil
				}
			}
		}
	})

	if err != nil {
		return err
	}

	// Re-marshal and unmarshal into caller's typed dest
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache: remap: %w", err)
	}
	return json.Unmarshal(b, dest)
}

// Close gracefully closes the Redis connection.
func (s *Service) Close() {
	if s != nil && s.client != nil {
		_ = s.client.Close()
		log.Println("[cache] Redis connection closed")
	}
}

// Client returns the underlying redis.Client if enabled, or nil.
func (s *Service) Client() *redis.Client {
	if s == nil || !s.enabled {
		return nil
	}
	return s.client
}

// Publish sends a message to the specified Redis Pub/Sub channel.
func (s *Service) Publish(ctx context.Context, channel string, message any) error {
	if s == nil || !s.enabled || s.client == nil {
		return nil
	}
	var data any = message
	switch m := message.(type) {
	case string, []byte:
		data = m
	default:
		b, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("cache: publish marshal error: %w", err)
		}
		data = b
	}
	return s.client.Publish(ctx, channel, data).Err()
}

// Subscribe opens a subscription on the specified Redis Pub/Sub channels.
func (s *Service) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	if s == nil || !s.enabled || s.client == nil {
		return nil
	}
	return s.client.Subscribe(ctx, channels...)
}
