package ratelimiter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TokenBucketLimiter implements the token bucket rate limiting algorithm
// Tokens are added to the bucket at a fixed rate, and each request consumes tokens
// This allows for burst traffic up to the bucket capacity
type TokenBucketLimiter struct {
	config       *Config
	buckets      sync.Map // map[string]*tokenBucket
	customLimits sync.Map // map[string]*Limit
}

// tokenBucket represents a token bucket for a specific key
type tokenBucket struct {
	tokens         float64
	capacity       float64
	refillRate     float64 // tokens per second
	lastRefillTime time.Time
	mu             sync.Mutex
}

// NewTokenBucketLimiter creates a new token bucket rate limiter
func NewTokenBucketLimiter(config *Config) *TokenBucketLimiter {
	if config == nil {
		config = DefaultConfig()
		config.Algorithm = TokenBucket
	}
	if config.DefaultLimit == nil {
		config.DefaultLimit = NewLimit(100, time.Minute)
	}

	return &TokenBucketLimiter{
		config: config,
	}
}

// Allow checks if a request should be allowed for the given key
func (t *TokenBucketLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return t.AllowN(ctx, key, 1)
}

// AllowN checks if N requests should be allowed for the given key
func (t *TokenBucketLimiter) AllowN(ctx context.Context, key string, n int) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("n must be positive, got %d", n)
	}

	limit := t.GetLimit(key)
	if limit == nil {
		return false, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()

	// Calculate refill rate (tokens per second)
	refillRate := float64(limit.RequestsPerWindow) / limit.WindowSize.Seconds()

	// Get bucket capacity (burst size)
	capacity := float64(limit.BurstSize)
	if capacity == 0 {
		capacity = float64(limit.RequestsPerWindow)
	}

	// Get or create bucket for this key
	bucketInterface, _ := t.buckets.LoadOrStore(key, &tokenBucket{
		tokens:         capacity,
		capacity:       capacity,
		refillRate:     refillRate,
		lastRefillTime: now,
	})

	bucket := bucketInterface.(*tokenBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens based on time elapsed
	elapsed := now.Sub(bucket.lastRefillTime).Seconds()
	bucket.tokens += elapsed * bucket.refillRate
	if bucket.tokens > bucket.capacity {
		bucket.tokens = bucket.capacity
	}
	bucket.lastRefillTime = now

	// Check if we have enough tokens
	if bucket.tokens < float64(n) {
		return false, nil
	}

	// Consume tokens
	bucket.tokens -= float64(n)
	return true, nil
}

// AllowWithInfo checks if a request should be allowed and returns detailed information
func (t *TokenBucketLimiter) AllowWithInfo(ctx context.Context, key string) (*Result, error) {
	limit := t.GetLimit(key)
	if limit == nil {
		return nil, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()

	refillRate := float64(limit.RequestsPerWindow) / limit.WindowSize.Seconds()
	capacity := float64(limit.BurstSize)
	if capacity == 0 {
		capacity = float64(limit.RequestsPerWindow)
	}

	bucketInterface, _ := t.buckets.LoadOrStore(key, &tokenBucket{
		tokens:         capacity,
		capacity:       capacity,
		refillRate:     refillRate,
		lastRefillTime: now,
	})

	bucket := bucketInterface.(*tokenBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens
	elapsed := now.Sub(bucket.lastRefillTime).Seconds()
	bucket.tokens += elapsed * bucket.refillRate
	if bucket.tokens > bucket.capacity {
		bucket.tokens = bucket.capacity
	}
	bucket.lastRefillTime = now

	allowed := bucket.tokens >= 1.0
	remaining := int(bucket.tokens)

	var retryAfter time.Duration
	if !allowed {
		// Calculate time needed to get one token
		tokensNeeded := 1.0 - bucket.tokens
		secondsNeeded := tokensNeeded / bucket.refillRate
		retryAfter = time.Duration(secondsNeeded * float64(time.Second))
	}

	if allowed {
		bucket.tokens -= 1.0
		remaining = int(bucket.tokens)
	}

	return &Result{
		Allowed:    allowed,
		Limit:      limit.RequestsPerWindow,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAt:    time.Time{}, // Token bucket doesn't have a fixed reset time
	}, nil
}

// Reset resets the rate limit for the given key
func (t *TokenBucketLimiter) Reset(ctx context.Context, key string) error {
	t.buckets.Delete(key)
	return nil
}

// GetLimit returns the current limit configuration for a key
func (t *TokenBucketLimiter) GetLimit(key string) *Limit {
	if customLimit, ok := t.customLimits.Load(key); ok {
		return customLimit.(*Limit)
	}
	return t.config.DefaultLimit
}

// SetLimit sets a custom limit for a specific key
func (t *TokenBucketLimiter) SetLimit(key string, limit *Limit) error {
	if limit == nil {
		return fmt.Errorf("limit cannot be nil")
	}
	if limit.RequestsPerWindow <= 0 {
		return fmt.Errorf("requests per window must be positive")
	}
	if limit.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive")
	}

	t.customLimits.Store(key, limit)

	// Reset the bucket to apply new limits
	t.buckets.Delete(key)

	return nil
}

// GetBucketInfo returns the current bucket information for debugging
func (t *TokenBucketLimiter) GetBucketInfo(key string) (tokens float64, capacity float64, exists bool) {
	bucketInterface, ok := t.buckets.Load(key)
	if !ok {
		return 0, 0, false
	}

	bucket := bucketInterface.(*tokenBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	return bucket.tokens, bucket.capacity, true
}

// Close cleans up resources
func (t *TokenBucketLimiter) Close() error {
	t.buckets.Range(func(key, value interface{}) bool {
		t.buckets.Delete(key)
		return true
	})

	t.customLimits.Range(func(key, value interface{}) bool {
		t.customLimits.Delete(key)
		return true
	})

	return nil
}
