package ratelimiter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FixedWindowLimiter implements a fixed window rate limiter
// It counts requests in fixed time windows and resets the counter at window boundaries
type FixedWindowLimiter struct {
	config       *Config
	windows      sync.Map // map[string]*fixedWindow
	customLimits sync.Map // map[string]*Limit
	mu           sync.RWMutex
}

// fixedWindow represents a fixed time window for a specific key
type fixedWindow struct {
	count      int
	windowStart time.Time
	mu         sync.Mutex
}

// NewFixedWindowLimiter creates a new fixed window rate limiter
func NewFixedWindowLimiter(config *Config) *FixedWindowLimiter {
	if config == nil {
		config = DefaultConfig()
	}
	if config.DefaultLimit == nil {
		config.DefaultLimit = NewLimit(100, time.Minute)
	}

	return &FixedWindowLimiter{
		config: config,
	}
}

// Allow checks if a request should be allowed for the given key
func (f *FixedWindowLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return f.AllowN(ctx, key, 1)
}

// AllowN checks if N requests should be allowed for the given key
func (f *FixedWindowLimiter) AllowN(ctx context.Context, key string, n int) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("n must be positive, got %d", n)
	}

	limit := f.GetLimit(key)
	if limit == nil {
		return false, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()

	// Get or create window for this key
	windowInterface, _ := f.windows.LoadOrStore(key, &fixedWindow{
		windowStart: now,
		count:       0,
	})

	window := windowInterface.(*fixedWindow)
	window.mu.Lock()
	defer window.mu.Unlock()

	// Check if we need to reset the window
	if now.Sub(window.windowStart) >= limit.WindowSize {
		window.count = 0
		window.windowStart = now
	}

	// Check if adding n requests would exceed the limit
	if window.count+n > limit.RequestsPerWindow {
		return false, nil
	}

	// Allow the request and increment counter
	window.count += n
	return true, nil
}

// AllowWithInfo checks if a request should be allowed and returns detailed information
func (f *FixedWindowLimiter) AllowWithInfo(ctx context.Context, key string) (*Result, error) {
	limit := f.GetLimit(key)
	if limit == nil {
		return nil, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()

	windowInterface, _ := f.windows.LoadOrStore(key, &fixedWindow{
		windowStart: now,
		count:       0,
	})

	window := windowInterface.(*fixedWindow)
	window.mu.Lock()
	defer window.mu.Unlock()

	// Check if we need to reset the window
	if now.Sub(window.windowStart) >= limit.WindowSize {
		window.count = 0
		window.windowStart = now
	}

	resetAt := window.windowStart.Add(limit.WindowSize)
	allowed := window.count < limit.RequestsPerWindow
	remaining := limit.RequestsPerWindow - window.count
	if remaining < 0 {
		remaining = 0
	}

	var retryAfter time.Duration
	if !allowed {
		retryAfter = time.Until(resetAt)
		if retryAfter < 0 {
			retryAfter = 0
		}
	}

	if allowed {
		window.count++
		remaining--
	}

	return &Result{
		Allowed:    allowed,
		Limit:      limit.RequestsPerWindow,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAt:    resetAt,
	}, nil
}

// Reset resets the rate limit for the given key
func (f *FixedWindowLimiter) Reset(ctx context.Context, key string) error {
	f.windows.Delete(key)
	return nil
}

// GetLimit returns the current limit configuration for a key
func (f *FixedWindowLimiter) GetLimit(key string) *Limit {
	// Check if there's a custom limit for this key
	if customLimit, ok := f.customLimits.Load(key); ok {
		return customLimit.(*Limit)
	}

	// Return the default limit
	return f.config.DefaultLimit
}

// SetLimit sets a custom limit for a specific key
func (f *FixedWindowLimiter) SetLimit(key string, limit *Limit) error {
	if limit == nil {
		return fmt.Errorf("limit cannot be nil")
	}
	if limit.RequestsPerWindow <= 0 {
		return fmt.Errorf("requests per window must be positive")
	}
	if limit.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive")
	}

	f.customLimits.Store(key, limit)
	return nil
}

// GetWindowInfo returns the current window information for debugging
func (f *FixedWindowLimiter) GetWindowInfo(key string) (count int, windowStart time.Time, exists bool) {
	windowInterface, ok := f.windows.Load(key)
	if !ok {
		return 0, time.Time{}, false
	}

	window := windowInterface.(*fixedWindow)
	window.mu.Lock()
	defer window.mu.Unlock()

	return window.count, window.windowStart, true
}

// Close cleans up resources
func (f *FixedWindowLimiter) Close() error {
	// Clear all windows
	f.windows.Range(func(key, value interface{}) bool {
		f.windows.Delete(key)
		return true
	})

	// Clear all custom limits
	f.customLimits.Range(func(key, value interface{}) bool {
		f.customLimits.Delete(key)
		return true
	})

	return nil
}
