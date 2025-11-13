package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucketLimiter_Allow(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,   // 10 tokens per second
			WindowSize:        time.Second,
			BurstSize:         5,    // Bucket capacity of 5
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow up to burst size (5 requests) immediately
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(ctx, key)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !allowed {
			t.Errorf("Request %d should be allowed (within burst)", i+1)
		}
	}

	// 6th request should be denied (bucket empty)
	allowed, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if allowed {
		t.Error("Request 6 should be denied (bucket empty)")
	}

	// Wait for tokens to refill (0.5 second = 5 tokens at 10/sec)
	time.Sleep(500 * time.Millisecond)

	// Should now have ~5 tokens available
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request should be allowed after refill (attempt %d)", i+1)
		}
	}
}

func TestTokenBucketLimiter_BurstHandling(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 2,    // 2 tokens per second
			WindowSize:        time.Second,
			BurstSize:         10,   // Large burst capacity
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "burst-key"

	// Should handle burst of 10 requests
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Burst request %d should be allowed", i+1)
		}
	}

	// Next request should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied after burst")
	}

	// Wait for 1 token to refill (0.5 second at 2/sec)
	time.Sleep(500 * time.Millisecond)

	// Should have 1 token
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after partial refill")
	}
}

func TestTokenBucketLimiter_AllowN(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
			BurstSize:         20,
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow 15 tokens
	allowed, err := limiter.AllowN(ctx, key, 15)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 15 tokens")
	}

	// Should allow 5 more tokens
	allowed, err = limiter.AllowN(ctx, key, 5)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Should allow 5 more tokens")
	}

	// Should deny 1 more token (bucket empty)
	allowed, err = limiter.AllowN(ctx, key, 1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if allowed {
		t.Error("Should deny token request (bucket empty)")
	}
}

func TestTokenBucketLimiter_AllowN_Invalid(t *testing.T) {
	limiter := NewTokenBucketLimiter(nil)
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

func TestTokenBucketLimiter_MultipleKeys(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
			BurstSize:         3,
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	key1 := "user1"
	key2 := "user2"

	// Use up key1's tokens
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

	// Key2 should still be allowed (independent bucket)
	allowed, _ = limiter.Allow(ctx, key2)
	if !allowed {
		t.Error("Key2 should be allowed (independent bucket)")
	}
}

func TestTokenBucketLimiter_SetLimit(t *testing.T) {
	limiter := NewTokenBucketLimiter(nil)
	defer limiter.Close()

	ctx := context.Background()
	key := "custom-key"

	// Set custom limit for this key
	customLimit := &Limit{
		RequestsPerWindow: 5,
		WindowSize:        time.Second,
		BurstSize:         2,
	}
	err := limiter.SetLimit(key, customLimit)
	if err != nil {
		t.Fatalf("Failed to set limit: %v", err)
	}

	// Should allow 2 requests (burst size)
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

func TestTokenBucketLimiter_SetLimit_Invalid(t *testing.T) {
	limiter := NewTokenBucketLimiter(nil)
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

func TestTokenBucketLimiter_Reset(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
			BurstSize:         3,
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Use up all tokens
	for i := 0; i < 3; i++ {
		limiter.Allow(ctx, key)
	}

	// Should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied before reset")
	}

	// Reset the bucket
	err := limiter.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Should have full bucket after reset
	for i := 0; i < 3; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request %d should be allowed after reset", i+1)
		}
	}
}

func TestTokenBucketLimiter_AllowWithInfo(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
			BurstSize:         5,
		},
	}

	limiter := NewTokenBucketLimiter(config)
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
	if result.Remaining != 4 {
		t.Errorf("Expected remaining 4, got %d", result.Remaining)
	}

	// Use up remaining tokens
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
	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}
}

func TestTokenBucketLimiter_Concurrent(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Second,
			BurstSize:         50,
		},
	}

	limiter := NewTokenBucketLimiter(config)
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

	// Should allow exactly burst size (50)
	if allowedCount != 50 {
		t.Errorf("Expected 50 allowed requests, got %d", allowedCount)
	}
}

func TestTokenBucketLimiter_ContinuousRefill(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,   // 10 tokens/second
			WindowSize:        time.Second,
			BurstSize:         5,
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "refill-key"

	// Use up all tokens
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, key)
	}

	// Should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied when bucket empty")
	}

	// Wait for 0.1 second (should get 1 token)
	time.Sleep(100 * time.Millisecond)

	// Should be allowed (1 token available)
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after 0.1s refill")
	}

	// Should be denied again
	allowed, _ = limiter.Allow(ctx, key)
	if allowed {
		t.Error("Should be denied again")
	}

	// Wait for 0.1 second more
	time.Sleep(100 * time.Millisecond)

	// Should be allowed again
	allowed, _ = limiter.Allow(ctx, key)
	if !allowed {
		t.Error("Should be allowed after another 0.1s refill")
	}
}

func TestTokenBucketLimiter_GetBucketInfo(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
			BurstSize:         5,
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// No bucket should exist initially
	_, _, exists := limiter.GetBucketInfo(key)
	if exists {
		t.Error("Bucket should not exist initially")
	}

	// Make a request
	limiter.Allow(ctx, key)

	// Bucket should exist
	tokens, capacity, exists := limiter.GetBucketInfo(key)
	if !exists {
		t.Error("Bucket should exist after request")
	}
	if capacity != 5 {
		t.Errorf("Expected capacity 5, got %f", capacity)
	}
	if tokens != 4 {
		t.Errorf("Expected 4 tokens remaining, got %f", tokens)
	}
}

func TestTokenBucketLimiter_DefaultBurstSize(t *testing.T) {
	config := &Config{
		Algorithm: TokenBucket,
		DefaultLimit: &Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
			BurstSize:         0, // Should default to RequestsPerWindow
		},
	}

	limiter := NewTokenBucketLimiter(config)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// Should allow 10 requests (defaults to RequestsPerWindow)
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 11th request should be denied
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		t.Error("Request 11 should be denied")
	}
}
