package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFixedWindowLimiter_Allow(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	limiter := NewFixedWindowLimiter(config)
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

	// Wait for window to reset
	time.Sleep(1100 * time.Millisecond)

	// Next request should be allowed after window reset
	allowed, err = limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Request should be allowed after window reset")
	}
}

func TestFixedWindowLimiter_AllowN(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow 5 requests
	allowed, err := limiter.AllowN(ctx, key, 5)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 5 requests")
	}

	// Should allow 5 more requests
	allowed, err = limiter.AllowN(ctx, key, 5)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 5 more requests")
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

func TestFixedWindowLimiter_AllowN_Invalid(t *testing.T) {
	limiter := NewFixedWindowLimiter(nil)
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

func TestFixedWindowLimiter_MultipleKeys(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 3,
			WindowSize:        time.Second,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	// Test that different keys have independent limits
	key1 := "user1"
	key2 := "user2"

	// Use up key1's limit
	for i := 0; i < 3; i++ {
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

func TestFixedWindowLimiter_SetLimit(t *testing.T) {
	limiter := NewFixedWindowLimiter(nil)
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

func TestFixedWindowLimiter_SetLimit_Invalid(t *testing.T) {
	limiter := NewFixedWindowLimiter(nil)
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

func TestFixedWindowLimiter_Reset(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Minute,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Use up the limit
	limiter.Allow(ctx, key)
	limiter.Allow(ctx, key)

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

func TestFixedWindowLimiter_AllowWithInfo(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	limiter := NewFixedWindowLimiter(config)
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
	if result.Remaining != 4 {
		t.Errorf("Expected remaining 4, got %d", result.Remaining)
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

func TestFixedWindowLimiter_Concurrent(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Second,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "concurrent-key"

	// Run 200 concurrent requests
	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
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

	// Exactly 100 should be allowed
	if allowedCount != 100 {
		t.Errorf("Expected 100 allowed requests, got %d", allowedCount)
	}
}

func TestFixedWindowLimiter_GetLimit(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 50,
			WindowSize:        time.Minute,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	// Test default limit
	limit := limiter.GetLimit("any-key")
	if limit.RequestsPerWindow != 50 {
		t.Errorf("Expected default limit 50, got %d", limit.RequestsPerWindow)
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

func TestFixedWindowLimiter_WindowBoundary(t *testing.T) {
	config := &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,
			WindowSize:        500 * time.Millisecond,
		},
	}

	limiter := NewFixedWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "boundary-key"

	// Use up limit in first window
	limiter.Allow(ctx, key)
	limiter.Allow(ctx, key)

	// Should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied in first window")
	}

	// Wait for window to expire
	time.Sleep(600 * time.Millisecond)

	// Should be allowed in new window
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed in new window")
	}
}

func TestNewLimit(t *testing.T) {
	limit := NewLimit(100, time.Minute)

	if limit.RequestsPerWindow != 100 {
		t.Errorf("Expected RequestsPerWindow 100, got %d", limit.RequestsPerWindow)
	}
	if limit.WindowSize != time.Minute {
		t.Errorf("Expected WindowSize 1 minute, got %v", limit.WindowSize)
	}
	if limit.BurstSize != 100 {
		t.Errorf("Expected BurstSize 100, got %d", limit.BurstSize)
	}
}

func TestLimit_WithBurst(t *testing.T) {
	limit := NewLimit(100, time.Minute).WithBurst(200)

	if limit.BurstSize != 200 {
		t.Errorf("Expected BurstSize 200, got %d", limit.BurstSize)
	}
}
