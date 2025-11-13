package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSlidingWindowLimiter_Allow(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
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

	// Wait for window to slide
	time.Sleep(1100 * time.Millisecond)

	// Next request should be allowed after old requests expire
	allowed, err = limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Request should be allowed after window slides")
	}
}

func TestSlidingWindowLimiter_SlidingBehavior(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 3,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "sliding-key"

	// Make 3 requests at t=0
	for i := 0; i < 3; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// Should be denied immediately
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied when limit reached")
	}

	// Wait 600ms (window is 1s)
	time.Sleep(600 * time.Millisecond)

	// Should still be denied (requests still in window)
	allowed, _ = limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should still be denied at t=600ms")
	}

	// Wait another 500ms (total 1100ms)
	time.Sleep(500 * time.Millisecond)

	// Now the first requests should have expired
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after first requests expire")
	}
}

func TestSlidingWindowLimiter_AllowN(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow 6 requests
	allowed, err := limiter.AllowN(ctx, key, 6)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 6 requests")
	}

	// Should allow 4 more requests
	allowed, err = limiter.AllowN(ctx, key, 4)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 4 more requests")
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

func TestSlidingWindowLimiter_MultipleKeys(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	key1 := "user1"
	key2 := "user2"

	// Use up key1's limit
	for i := 0; i < 2; i++ {
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

func TestSlidingWindowLimiter_SetLimit(t *testing.T) {
	limiter := NewSlidingWindowLimiter(nil)
	defer limiter.Close()

	ctx := context.Background()
	key := "custom-key"

	// Set custom limit for this key
	customLimit := &Limit{
		RequestsPerWindow: 3,
		WindowSize:        500 * time.Millisecond,
	}
	err := limiter.SetLimit(key, customLimit)
	if err != nil {
		t.Fatalf("Failed to set limit: %v", err)
	}

	// Should allow 3 requests
	for i := 0; i < 3; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Request 4 should be denied with custom limit")
	}
}

func TestSlidingWindowLimiter_Reset(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Minute,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
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

func TestSlidingWindowLimiter_AllowWithInfo(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
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

func TestSlidingWindowLimiter_Concurrent(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 50,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "concurrent-key"

	// Run 100 concurrent requests
	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
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

	// Exactly 50 should be allowed
	if allowedCount != 50 {
		t.Errorf("Expected 50 allowed requests, got %d", allowedCount)
	}
}

func TestSlidingWindowLimiter_GetLogInfo(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// No log should exist initially
	count, timestamps, exists := limiter.GetLogInfo(key)
	if exists {
		t.Error("Log should not exist initially")
	}

	// Make some requests
	for i := 0; i < 3; i++ {
		limiter.Allow(ctx, key)
	}

	// Log should exist with 3 entries
	count, timestamps, exists = limiter.GetLogInfo(key)
	if !exists {
		t.Error("Log should exist after requests")
	}
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}
	if len(timestamps) != 3 {
		t.Errorf("Expected 3 timestamps, got %d", len(timestamps))
	}
}

func TestSlidingWindowLimiter_MemoryCleanup(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        100 * time.Millisecond,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "cleanup-key"

	// Make requests
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, key)
	}

	// Verify log exists
	count, _, exists := limiter.GetLogInfo(key)
	if !exists || count != 5 {
		t.Error("Log should exist with 5 entries")
	}

	// Wait for timestamps to expire
	time.Sleep(150 * time.Millisecond)

	// Make a new request to trigger cleanup
	limiter.Allow(ctx, key)

	// Log should have only 1 entry (the new request)
	count, _, _ = limiter.GetLogInfo(key)
	if count != 1 {
		t.Errorf("Expected count 1 after cleanup, got %d", count)
	}
}

func TestSlidingWindowLimiter_NoBoundaryIssue(t *testing.T) {
	config := &Config{
		Algorithm: SlidingWindowLog,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Second,
		},
	}

	limiter := NewSlidingWindowLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "boundary-key"

	// Make 2 requests at end of "window"
	limiter.Allow(ctx, key)
	time.Sleep(900 * time.Millisecond)
	limiter.Allow(ctx, key)

	// Immediately make another request (still within sliding window)
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied - both requests still in sliding window")
	}

	// Wait a bit more for first request to expire
	time.Sleep(200 * time.Millisecond)

	// Now should be allowed (first request outside window)
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after first request expires from window")
	}
}
