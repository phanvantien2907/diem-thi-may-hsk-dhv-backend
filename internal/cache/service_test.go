package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheService_DisabledGracefulFallback(t *testing.T) {
	// Service without redis should work gracefully
	svc := NewService(Options{Addr: ""})
	assert.False(t, svc.enabled)

	ctx := context.Background()

	// Set should not fail
	err := svc.Set(ctx, "test:key", "value", 1*time.Minute)
	assert.NoError(t, err)

	// SetWithJitter should not fail
	err = svc.SetWithJitter(ctx, "test:jitter", "val", 1*time.Minute, 0.35)
	assert.NoError(t, err)

	// Get should return miss
	var dest string
	hit, err := svc.Get(ctx, "test:key", &dest)
	assert.NoError(t, err)
	assert.False(t, hit)

	// Delete should not fail
	err = svc.Delete(ctx, "test:key")
	assert.NoError(t, err)

	// DeletePrefix should not fail
	err = svc.DeletePrefix(ctx, "test:")
	assert.NoError(t, err)

	// Lock operations should degrade gracefully
	token, acquired, err := svc.AcquireLock(ctx, "lock:test", 5*time.Second)
	assert.NoError(t, err)
	assert.False(t, acquired)
	assert.Empty(t, token)

	err = svc.ReleaseLock(ctx, "lock:test", "any-token")
	assert.NoError(t, err)

	// Remember should invoke queryFn directly and return result
	var remembered string
	called := false
	err = svc.Remember(ctx, "test:remember", 1*time.Minute, &remembered, func() (any, error) {
		called = true
		return "hello world", nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "hello world", remembered)
}

func TestCacheService_JitterCalculation(t *testing.T) {
	svc := NewService(Options{Addr: ""})

	// Test zero or negative TTL
	assert.Equal(t, time.Duration(0), svc.jitter(0, 0.10))
	assert.Equal(t, -5*time.Second, svc.jitter(-5*time.Second, 0.10))

	// Test default jitter ±10%
	baseTTL := 10 * time.Minute
	for i := 0; i < 50; i++ {
		j := svc.jitter(baseTTL, 0)
		assert.GreaterOrEqual(t, j, 9*time.Minute)
		assert.LessOrEqual(t, j, 11*time.Minute)
	}

	// Test custom batch jitter ±35% (e.g. for exam sessions)
	for i := 0; i < 50; i++ {
		j := svc.jitter(baseTTL, 0.35)
		assert.GreaterOrEqual(t, j, time.Duration(float64(baseTTL)*0.65))
		assert.LessOrEqual(t, j, time.Duration(float64(baseTTL)*1.35))
	}
}

func TestCacheService_DistributedLock_RealRedis(t *testing.T) {
	// Connect to local docker/dev Redis if available
	svc := NewService(Options{Addr: "localhost:6379", DB: 15})
	if !svc.enabled || svc.client.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration lock test")
	}
	defer svc.Close()

	ctx := context.Background()
	lockKey := "lock:test:hsk:exam:sessions"
	_ = svc.Delete(ctx, lockKey)

	// 1. Instance 1 acquires lock
	token1, ok1, err := svc.AcquireLock(ctx, lockKey, 2*time.Second)
	assert.NoError(t, err)
	assert.True(t, ok1)
	assert.NotEmpty(t, token1)

	// 2. Instance 2 attempts to acquire same lock -> fails (NX)
	token2, ok2, err := svc.AcquireLock(ctx, lockKey, 2*time.Second)
	assert.NoError(t, err)
	assert.False(t, ok2)
	assert.Empty(t, token2)

	// 3. Instance 2 tries to release with wrong token -> does not release
	err = svc.ReleaseLock(ctx, lockKey, "wrong-token")
	assert.NoError(t, err)

	// Still locked
	_, ok3, _ := svc.AcquireLock(ctx, lockKey, 2*time.Second)
	assert.False(t, ok3)

	// 4. Instance 1 releases with correct token
	err = svc.ReleaseLock(ctx, lockKey, token1)
	assert.NoError(t, err)

	// 5. Now lock can be re-acquired
	token4, ok4, err := svc.AcquireLock(ctx, lockKey, 2*time.Second)
	assert.NoError(t, err)
	assert.True(t, ok4)
	_ = svc.ReleaseLock(ctx, lockKey, token4)
}

func TestCacheService_ColdStart_MultiInstanceDeduplication(t *testing.T) {
	// Simulate 2 separate backend instances connecting to the same Redis
	inst1 := NewService(Options{Addr: "localhost:6379", DB: 15})
	if !inst1.enabled || inst1.client.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available on localhost:6379, skipping multi-instance test")
	}
	defer inst1.Close()

	inst2 := NewService(Options{Addr: "localhost:6379", DB: 15})
	defer inst2.Close()

	ctx := context.Background()
	testKey := "hsk:test:coldstart:provinces"
	_ = inst1.Delete(ctx, testKey, "lock:"+testKey)

	var queryCount int64
	var queryMu sync.Mutex

	expensiveQuery := func() (any, error) {
		queryMu.Lock()
		queryCount++
		queryMu.Unlock()
		time.Sleep(150 * time.Millisecond) // Simulate DB query latency
		return []string{"Hà Nội", "Nghệ An", "TP. Hồ Chí Minh"}, nil
	}

	var wg sync.WaitGroup
	var res1 []string
	var res2 []string

	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = inst1.RememberWithJitter(ctx, testKey, 5*time.Minute, 0.35, &res1, expensiveQuery)
	}()
	go func() {
		defer wg.Done()
		_ = inst2.RememberWithJitter(ctx, testKey, 5*time.Minute, 0.35, &res2, expensiveQuery)
	}()

	wg.Wait()

	assert.Equal(t, []string{"Hà Nội", "Nghệ An", "TP. Hồ Chí Minh"}, res1)
	assert.Equal(t, []string{"Hà Nội", "Nghệ An", "TP. Hồ Chí Minh"}, res2)
	// Distributed lock ensures the query was only called ONCE across the 2 instances
	assert.Equal(t, int64(1), queryCount)

	_ = inst1.Delete(ctx, testKey, "lock:"+testKey)
}


