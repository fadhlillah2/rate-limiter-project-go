package ratelimiter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SlidingWindowLimiter implements a sliding window log rate limiter
// It tracks individual request timestamps and provides more accurate rate limiting
// compared to fixed window, but uses more memory
type SlidingWindowLimiter struct {
	config       *Config
	logs         sync.Map // map[string]*requestLog
	customLimits sync.Map // map[string]*Limit
}

// requestLog stores timestamps of requests for a specific key
type requestLog struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter
func NewSlidingWindowLimiter(config *Config) *SlidingWindowLimiter {
	if config == nil {
		config = DefaultConfig()
		config.Algorithm = SlidingWindowLog
	}
	if config.DefaultLimit == nil {
		config.DefaultLimit = NewLimit(100, time.Minute)
	}

	limiter := &SlidingWindowLimiter{
		config: config,
	}

	// Start background cleanup goroutine
	go limiter.cleanup()

	return limiter
}

// Allow checks if a request should be allowed for the given key
func (s *SlidingWindowLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return s.AllowN(ctx, key, 1)
}

// AllowN checks if N requests should be allowed for the given key
func (s *SlidingWindowLimiter) AllowN(ctx context.Context, key string, n int) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("n must be positive, got %d", n)
	}

	limit := s.GetLimit(key)
	if limit == nil {
		return false, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()
	windowStart := now.Add(-limit.WindowSize)

	// Get or create request log for this key
	logInterface, _ := s.logs.LoadOrStore(key, &requestLog{
		timestamps: make([]time.Time, 0),
	})

	log := logInterface.(*requestLog)
	log.mu.Lock()
	defer log.mu.Unlock()

	// Remove timestamps outside the current window
	validTimestamps := make([]time.Time, 0, len(log.timestamps))
	for _, ts := range log.timestamps {
		if ts.After(windowStart) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	log.timestamps = validTimestamps

	// Check if adding n requests would exceed the limit
	if len(log.timestamps)+n > limit.RequestsPerWindow {
		return false, nil
	}

	// Add n timestamps for the new requests
	for i := 0; i < n; i++ {
		log.timestamps = append(log.timestamps, now)
	}

	return true, nil
}

// AllowWithInfo checks if a request should be allowed and returns detailed information
func (s *SlidingWindowLimiter) AllowWithInfo(ctx context.Context, key string) (*Result, error) {
	limit := s.GetLimit(key)
	if limit == nil {
		return nil, fmt.Errorf("no limit configured for key: %s", key)
	}

	now := time.Now()
	windowStart := now.Add(-limit.WindowSize)

	logInterface, _ := s.logs.LoadOrStore(key, &requestLog{
		timestamps: make([]time.Time, 0),
	})

	log := logInterface.(*requestLog)
	log.mu.Lock()
	defer log.mu.Unlock()

	// Remove timestamps outside the current window
	validTimestamps := make([]time.Time, 0, len(log.timestamps))
	var oldestInWindow time.Time
	for _, ts := range log.timestamps {
		if ts.After(windowStart) {
			validTimestamps = append(validTimestamps, ts)
			if oldestInWindow.IsZero() || ts.Before(oldestInWindow) {
				oldestInWindow = ts
			}
		}
	}
	log.timestamps = validTimestamps

	allowed := len(log.timestamps) < limit.RequestsPerWindow
	remaining := limit.RequestsPerWindow - len(log.timestamps)
	if remaining < 0 {
		remaining = 0
	}

	var retryAfter time.Duration
	var resetAt time.Time

	if !allowed && !oldestInWindow.IsZero() {
		// The window will reset when the oldest request expires
		resetAt = oldestInWindow.Add(limit.WindowSize)
		retryAfter = time.Until(resetAt)
		if retryAfter < 0 {
			retryAfter = 0
		}
	} else {
		resetAt = now.Add(limit.WindowSize)
	}

	if allowed {
		log.timestamps = append(log.timestamps, now)
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
func (s *SlidingWindowLimiter) Reset(ctx context.Context, key string) error {
	s.logs.Delete(key)
	return nil
}

// GetLimit returns the current limit configuration for a key
func (s *SlidingWindowLimiter) GetLimit(key string) *Limit {
	if customLimit, ok := s.customLimits.Load(key); ok {
		return customLimit.(*Limit)
	}
	return s.config.DefaultLimit
}

// SetLimit sets a custom limit for a specific key
func (s *SlidingWindowLimiter) SetLimit(key string, limit *Limit) error {
	if limit == nil {
		return fmt.Errorf("limit cannot be nil")
	}
	if limit.RequestsPerWindow <= 0 {
		return fmt.Errorf("requests per window must be positive")
	}
	if limit.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive")
	}

	s.customLimits.Store(key, limit)
	return nil
}

// GetLogInfo returns the current log information for debugging
func (s *SlidingWindowLimiter) GetLogInfo(key string) (count int, timestamps []time.Time, exists bool) {
	logInterface, ok := s.logs.Load(key)
	if !ok {
		return 0, nil, false
	}

	log := logInterface.(*requestLog)
	log.mu.Lock()
	defer log.mu.Unlock()

	// Return a copy of timestamps
	timestampsCopy := make([]time.Time, len(log.timestamps))
	copy(timestampsCopy, log.timestamps)

	return len(log.timestamps), timestampsCopy, true
}

// cleanup periodically removes old logs to prevent memory leaks
func (s *SlidingWindowLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.logs.Range(func(key, value interface{}) bool {
			log := value.(*requestLog)
			log.mu.Lock()

			limit := s.GetLimit(key.(string))
			if limit == nil {
				log.mu.Unlock()
				return true
			}

			now := time.Now()
			windowStart := now.Add(-limit.WindowSize)

			// If all timestamps are old, delete the entire log
			if len(log.timestamps) > 0 && log.timestamps[len(log.timestamps)-1].Before(windowStart) {
				log.mu.Unlock()
				s.logs.Delete(key)
				return true
			}

			log.mu.Unlock()
			return true
		})
	}
}

// Close cleans up resources
func (s *SlidingWindowLimiter) Close() error {
	s.logs.Range(func(key, value interface{}) bool {
		s.logs.Delete(key)
		return true
	})

	s.customLimits.Range(func(key, value interface{}) bool {
		s.customLimits.Delete(key)
		return true
	})

	return nil
}
