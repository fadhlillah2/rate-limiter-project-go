package ratelimiter

import (
	"context"
	"time"
)

// RateLimiter defines the interface for rate limiting implementations
type RateLimiter interface {
	// Allow checks if a request should be allowed for the given key
	// Returns true if allowed, false if rate limit exceeded
	Allow(ctx context.Context, key string) (bool, error)

	// AllowN checks if N requests should be allowed for the given key
	// Returns true if allowed, false if rate limit exceeded
	AllowN(ctx context.Context, key string, n int) (bool, error)

	// Reset resets the rate limit for the given key
	Reset(ctx context.Context, key string) error

	// GetLimit returns the current limit configuration for a key
	GetLimit(key string) *Limit

	// SetLimit sets a custom limit for a specific key
	SetLimit(key string, limit *Limit) error

	// Close cleans up resources
	Close() error
}

// Limit defines the rate limit configuration
type Limit struct {
	// RequestsPerWindow is the maximum number of requests allowed in the time window
	RequestsPerWindow int

	// WindowSize is the duration of the rate limit window
	WindowSize time.Duration

	// BurstSize is the maximum burst size (for token bucket algorithm)
	// If 0, defaults to RequestsPerWindow
	BurstSize int
}

// Result contains information about a rate limit check
type Result struct {
	// Allowed indicates if the request is allowed
	Allowed bool

	// Limit is the maximum number of requests allowed
	Limit int

	// Remaining is the number of requests remaining in the current window
	Remaining int

	// RetryAfter is the duration to wait before retrying (if not allowed)
	RetryAfter time.Duration

	// ResetAt is when the rate limit window resets
	ResetAt time.Time
}

// Algorithm represents the type of rate limiting algorithm
type Algorithm string

const (
	// FixedWindow uses a fixed time window counter
	FixedWindow Algorithm = "fixed_window"

	// SlidingWindowLog tracks individual request timestamps
	SlidingWindowLog Algorithm = "sliding_window_log"

	// SlidingWindowCounter uses a hybrid approach with weighted counters
	SlidingWindowCounter Algorithm = "sliding_window_counter"

	// TokenBucket implements the token bucket algorithm
	TokenBucket Algorithm = "token_bucket"

	// LeakyBucket implements the leaky bucket algorithm
	LeakyBucket Algorithm = "leaky_bucket"
)

// Config holds the configuration for a rate limiter
type Config struct {
	// Algorithm specifies which rate limiting algorithm to use
	Algorithm Algorithm

	// DefaultLimit is the default rate limit applied to all keys
	DefaultLimit *Limit

	// EnableMetrics enables metrics collection
	EnableMetrics bool

	// EnableLogging enables detailed logging
	EnableLogging bool
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Algorithm: FixedWindow,
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
			BurstSize:         0,
		},
		EnableMetrics: false,
		EnableLogging: false,
	}
}

// NewLimit creates a new Limit with the given parameters
func NewLimit(requestsPerWindow int, windowSize time.Duration) *Limit {
	return &Limit{
		RequestsPerWindow: requestsPerWindow,
		WindowSize:        windowSize,
		BurstSize:         requestsPerWindow,
	}
}

// WithBurst sets the burst size for the limit
func (l *Limit) WithBurst(burstSize int) *Limit {
	l.BurstSize = burstSize
	return l
}
