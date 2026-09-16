package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *RedisLimiter) {
	// Start miniredis server
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	// Create Redis limiter
	config := &Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	redisConfig := &RedisConfig{
		Addresses: []string{mr.Addr()},
		PoolSize:  10,
	}

	limiter, err := NewRedisLimiter(redisConfig, config)
	if err != nil {
		mr.Close()
		t.Fatalf("Failed to create Redis limiter: %v", err)
	}

	return mr, limiter
}

func TestNewRedisLimiter(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	config := &Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	}

	redisConfig := &RedisConfig{
		Addresses: []string{mr.Addr()},
		PoolSize:  10,
	}

	limiter, err := NewRedisLimiter(redisConfig, config)
	if err != nil {
		t.Fatalf("Failed to create Redis limiter: %v", err)
	}
	defer limiter.Close()

	if limiter == nil {
		t.Error("Limiter should not be nil")
	}
}

func TestNewRedisLimiter_NilConfig(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisConfig := &RedisConfig{
		Addresses: []string{mr.Addr()},
	}

	// Should use default config
	limiter, err := NewRedisLimiter(redisConfig, nil)
	if err != nil {
		t.Fatalf("Should handle nil config: %v", err)
	}
	defer limiter.Close()

	limit := limiter.GetLimit("test")
	if limit.RequestsPerWindow != 100 {
		t.Errorf("Expected default limit 100, got %d", limit.RequestsPerWindow)
	}
}

func TestNewRedisLimiter_NoAddress(t *testing.T) {
	config := &Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	}

	redisConfig := &RedisConfig{
		Addresses: []string{},
	}

	_, err := NewRedisLimiter(redisConfig, config)
	if err == nil {
		t.Error("Should return error when no Redis address provided")
	}
}

func TestNewRedisLimiter_NilRedisConfig(t *testing.T) {
	config := &Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	}

	_, err := NewRedisLimiter(nil, config)
	if err == nil {
		t.Error("Should return error when Redis config is nil")
	}
}

func TestRedisLimiter_Allow(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// First 5 requests should be allowed
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(ctx, key)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	allowed, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if allowed {
		t.Error("Request 6 should be denied")
	}
}

func TestRedisLimiter_AllowN(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow 3 requests
	allowed, err := limiter.AllowN(ctx, key, 3)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 3 requests")
	}

	// Should allow 2 more requests
	allowed, err = limiter.AllowN(ctx, key, 2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 2 more requests")
	}

	// Should deny 1 more request (would exceed limit)
	allowed, err = limiter.AllowN(ctx, key, 1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if allowed {
		t.Error("Should deny request that exceeds limit")
	}
}

func TestRedisLimiter_AllowN_Invalid(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Test with invalid n (zero)
	_, err := limiter.AllowN(ctx, key, 0)
	if err == nil {
		t.Error("Should return error for n=0")
	}

	// Test with invalid n (negative)
	_, err = limiter.AllowN(ctx, key, -1)
	if err == nil {
		t.Error("Should return error for negative n")
	}
}

func TestRedisLimiter_AllowN_RejectedBatchConsumesNothing(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	for _, step := range []struct {
		n       int
		allowed bool
	}{{6, false}, {3, true}, {3, false}, {2, true}, {1, false}} {
		allowed, err := limiter.AllowN(ctx, "batch", step.n)
		if err != nil || allowed != step.allowed {
			t.Fatalf("AllowN(%d) = %v, %v; want %v", step.n, allowed, err, step.allowed)
		}
	}
}

func TestRedisLimiter_SubsecondWindow(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	const window = 500 * time.Millisecond
	if err := limiter.SetLimit("fractional", NewLimit(1, window)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if allowed, err := limiter.Allow(ctx, "fractional"); err != nil || !allowed {
		t.Fatalf("First request = %v, %v; want allowed", allowed, err)
	}
	if ttl := mr.TTL("ratelimit:fractional"); ttl != window {
		t.Fatalf("TTL = %v; want %v", ttl, window)
	}
	result, err := limiter.AllowWithInfo(ctx, "fractional")
	if err != nil {
		t.Fatal(err)
	}
	if result.Allowed || result.RetryAfter <= 0 || result.RetryAfter > window {
		t.Fatalf("Denied result = %+v; want positive retry <= %v", result, window)
	}
	mr.FastForward(window)
	if allowed, err := limiter.Allow(ctx, "fractional"); err != nil || !allowed {
		t.Fatalf("Request after expiry = %v, %v; want allowed", allowed, err)
	}
}

func TestRedisLimiter_SubmillisecondWindow(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	if err := limiter.SetLimit("tiny", NewLimit(1, 500*time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if allowed, err := limiter.Allow(context.Background(), "tiny"); err != nil || !allowed {
		t.Fatalf("First request = %v, %v; want allowed", allowed, err)
	}
	if ttl := mr.TTL("ratelimit:tiny"); ttl != time.Millisecond {
		t.Fatalf("TTL = %v; want minimum Redis resolution 1ms", ttl)
	}
}

func TestRedisLimiter_BatchesAtSameMillisecond(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	const now = 1000
	const window = 500
	for _, nonce := range []string{"batch-a", "batch-b"} {
		if _, err := redisSlidingWindowScript.Run(ctx, limiter.client, []string{"same-time"},
			now, now-window, 4, window, 2, nonce).Result(); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := limiter.client.ZRangeWithScores(ctx, "same-time", 0, -1).Result()
	if err != nil || len(entries) != 4 {
		t.Fatalf("Same-time batches stored %d entries, %v; want 4", len(entries), err)
	}
	for _, entry := range entries {
		if entry.Score != now {
			t.Fatalf("Score = %v; want fixed timestamp %d", entry.Score, now)
		}
	}
	result, err := redisSlidingWindowScript.Run(ctx, limiter.client, []string{"same-time"},
		now, now-window, 4, window, 1, "batch-c").Result()
	if err != nil {
		t.Fatal(err)
	}
	if values := result.([]interface{}); values[0].(int64) != 0 || values[1].(int64) != 4 {
		t.Fatalf("Request after two batches = %v; want denied with count 4", result)
	}
}

func TestRedisMillisecondsDuration(t *testing.T) {
	const maxDuration = time.Duration(1<<63 - 1)
	for _, tc := range []struct {
		milliseconds int64
		want         time.Duration
	}{{-1, -time.Millisecond}, {500, 500 * time.Millisecond}, {maxDuration.Milliseconds() + 1, maxDuration}} {
		if got := redisMillisecondsDuration(tc.milliseconds); got != tc.want {
			t.Errorf("Duration(%dms) = %v; want %v", tc.milliseconds, got, tc.want)
		}
	}
}

func TestRedisLimiter_AllowWithInfo(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// First request
	result, err := limiter.AllowWithInfo(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("First request should be allowed")
	}
	if result.Limit != 5 {
		t.Errorf("Expected limit 5, got %d", result.Limit)
	}
	if result.Remaining < 0 || result.Remaining > 5 {
		t.Errorf("Expected remaining 0-5, got %d", result.Remaining)
	}

	// Use up remaining requests
	for i := 0; i < 4; i++ {
		limiter.Allow(ctx, key)
	}

	// Next request should be denied
	result, err = limiter.AllowWithInfo(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Allowed {
		t.Error("Request should be denied")
	}
	if result.Remaining != 0 {
		t.Errorf("Expected remaining 0, got %d", result.Remaining)
	}
	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}
}

func TestRedisLimiter_AllowWithFixedWindow(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "fixed-window-key"

	// Test fixed window implementation
	result, err := limiter.AllowWithFixedWindow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("First request should be allowed")
	}

	// Make more requests
	for i := 0; i < 4; i++ {
		limiter.AllowWithFixedWindow(ctx, key)
	}

	// Should be denied
	result, err = limiter.AllowWithFixedWindow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Allowed {
		t.Error("Request should be denied after limit")
	}
}

func TestRedisLimiter_FixedWindowFractionalDuration(t *testing.T) {
	for _, window := range []time.Duration{1500 * time.Millisecond, 500 * time.Millisecond, 500 * time.Microsecond} {
		t.Run(window.String(), func(t *testing.T) {
			mr, limiter := setupTestRedis(t)
			defer mr.Close()
			defer limiter.Close()
			if err := limiter.SetLimit("fractional-fixed", NewLimit(1, window)); err != nil {
				t.Fatal(err)
			}
			result, err := limiter.AllowWithFixedWindow(context.Background(), "fractional-fixed")
			if err != nil || !result.Allowed || result.Remaining != 0 {
				t.Fatalf("First fixed-window result = %+v, %v", result, err)
			}
			wantTTL := window
			if window < time.Millisecond {
				wantTTL = time.Millisecond
			}
			keys := mr.Keys()
			if len(keys) != 1 || mr.TTL(keys[0]) != wantTTL {
				t.Fatalf("Keys=%v; want one key with TTL %v", keys, wantTTL)
			}
		})
	}
}

func TestRedisLimiter_MultipleKeys(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()

	key1 := "user1"
	key2 := "user2"

	// Use up key1's limit
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(ctx, key1)
		if !allowed {
			t.Errorf("Key1 request %d should be allowed", i+1)
		}
	}

	// Key1 should be rate limited
	allowed, _ := limiter.Allow(ctx, key1)
	if allowed {
		t.Error("Key1 should be rate limited")
	}

	// Key2 should still be allowed
	allowed, _ = limiter.Allow(ctx, key2)
	if !allowed {
		t.Error("Key2 should be allowed (independent limit)")
	}
}

func TestRedisLimiter_SetLimit(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "custom-key"

	// Set custom limit for this key
	customLimit := &Limit{
		RequestsPerWindow: 2,
		WindowSize:        time.Second,
	}
	err := limiter.SetLimit(key, customLimit)
	if err != nil {
		t.Fatalf("Failed to set limit: %v", err)
	}

	// Should allow 2 requests
	for i := 0; i < 2; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 3rd request should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Request 3 should be denied with custom limit")
	}
}

func TestRedisLimiter_SetLimit_Invalid(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	key := "test-key"

	// Test nil limit
	err := limiter.SetLimit(key, nil)
	if err == nil {
		t.Error("Should return error for nil limit")
	}

	// Test invalid requests per window
	err = limiter.SetLimit(key, &Limit{
		RequestsPerWindow: 0,
		WindowSize:        time.Second,
	})
	if err == nil {
		t.Error("Should return error for zero requests per window")
	}

	// Test invalid window size
	err = limiter.SetLimit(key, &Limit{
		RequestsPerWindow: 10,
		WindowSize:        0,
	})
	if err == nil {
		t.Error("Should return error for zero window size")
	}
}

func TestRedisLimiter_Reset(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Use up the limit
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, key)
	}

	// Should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied before reset")
	}

	// Reset the limit
	err := limiter.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Should be allowed after reset
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after reset")
	}
}

func TestRedisLimiter_GetLimit(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	// Test default limit
	limit := limiter.GetLimit("any-key")
	if limit.RequestsPerWindow != 5 {
		t.Errorf("Expected default limit 5, got %d", limit.RequestsPerWindow)
	}

	// Test custom limit
	customKey := "custom-key"
	customLimit := &Limit{
		RequestsPerWindow: 25,
		WindowSize:        30 * time.Second,
	}
	limiter.SetLimit(customKey, customLimit)

	limit = limiter.GetLimit(customKey)
	if limit.RequestsPerWindow != 25 {
		t.Errorf("Expected custom limit 25, got %d", limit.RequestsPerWindow)
	}
}

func TestRedisLimiter_Concurrent(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "concurrent-key"

	// Run 20 concurrent requests (limit is 5)
	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _ := limiter.Allow(ctx, key)
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Should allow exactly 5 requests
	if allowedCount != 5 {
		t.Errorf("Expected 5 allowed requests, got %d", allowedCount)
	}
}

func TestRedisLimiter_GetClient(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	client := limiter.GetClient()
	if client == nil {
		t.Error("Client should not be nil")
	}

	// Test that client works
	ctx := context.Background()
	err := client.Ping(ctx).Err()
	if err != nil {
		t.Errorf("Redis client should be functional: %v", err)
	}
}

func TestRedisLimiter_Close(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()

	err := limiter.Close()
	if err != nil {
		t.Errorf("Close should not return error: %v", err)
	}

	// After closing, operations should fail
	ctx := context.Background()
	_, err = limiter.Allow(ctx, "test")
	if err == nil {
		t.Error("Operations after close should fail")
	}
}

func TestRedisLimiter_SlidingWindow(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	ctx := context.Background()
	key := "sliding-key"

	// Make 5 requests
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(ctx, key)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// Should be denied immediately
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied when limit reached")
	}

	// Fast forward time in miniredis
	mr.FastForward(2 * time.Second)

	// Now should be allowed (sliding window expired)
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after window expires")
	}
}

func TestRedisLimiter_BuildKey(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()

	key := limiter.buildKey("test")
	expected := "ratelimit:test"
	if key != expected {
		t.Errorf("Expected key '%s', got '%s'", expected, key)
	}
}

func TestRedisLimiter_ClusterMode(t *testing.T) {
	// Start multiple miniredis instances for cluster simulation
	mr1, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis 1: %v", err)
	}
	defer mr1.Close()

	mr2, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis 2: %v", err)
	}
	defer mr2.Close()

	config := &Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	// Test cluster mode with multiple addresses
	redisConfig := &RedisConfig{
		Addresses: []string{mr1.Addr(), mr2.Addr()},
		PoolSize:  10,
	}

	limiter, err := NewRedisLimiter(redisConfig, config)
	if err != nil {
		t.Fatalf("Failed to create cluster limiter: %v", err)
	}
	defer limiter.Close()

	// Basic operation should work
	ctx := context.Background()
	allowed, err := limiter.Allow(ctx, "test-key")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("First request should be allowed")
	}
}
