package middleware

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

// KeyExtractor is a function that extracts the rate limit key from a request
type KeyExtractor func(*http.Request) string

// RateLimitMiddleware provides HTTP middleware for rate limiting
type RateLimitMiddleware struct {
	limiter      ratelimiter.RateLimiter
	keyExtractor KeyExtractor
	onLimitFunc  func(w http.ResponseWriter, r *http.Request, retryAfter string)
	logger       *log.Logger
}

// Config holds configuration for the rate limit middleware
type Config struct {
	// Limiter is the rate limiter implementation to use
	Limiter ratelimiter.RateLimiter

	// KeyExtractor extracts the rate limit key from the request
	// If nil, defaults to IP-based extraction
	KeyExtractor KeyExtractor

	// OnLimit is called when a request is rate limited
	// If nil, returns a default 429 response
	OnLimit func(w http.ResponseWriter, r *http.Request, retryAfter string)

	// Logger for logging rate limit events
	Logger *log.Logger

	// SkipSuccessfulRequests if true, only counts failed requests
	SkipSuccessfulRequests bool
}

// New creates a new rate limit middleware
func New(config *Config) *RateLimitMiddleware {
	if config.KeyExtractor == nil {
		config.KeyExtractor = IPKeyExtractor
	}

	if config.OnLimit == nil {
		config.OnLimit = DefaultOnLimit
	}

	return &RateLimitMiddleware{
		limiter:      config.Limiter,
		keyExtractor: config.KeyExtractor,
		onLimitFunc:  config.OnLimit,
		logger:       config.Logger,
	}
}

// Handler returns an HTTP middleware handler
func (m *RateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract key for rate limiting
		key := m.keyExtractor(r)

		// Try to use AllowWithInfo if available for detailed information
		if limiterWithInfo, ok := m.limiter.(interface {
			AllowWithInfo(context.Context, string) (*ratelimiter.Result, error)
		}); ok {
			result, err := limiterWithInfo.AllowWithInfo(r.Context(), key)
			if err != nil {
				if m.logger != nil {
					m.logger.Printf("Rate limiter error for key %s: %v", key, err)
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !result.Allowed {
				retryAfter := fmt.Sprintf("%.0f", result.RetryAfter.Seconds())
				if retryAfter == "0" {
					retryAfter = "1"
				}
				m.onLimitFunc(w, r, retryAfter)
				return
			}

			// Set rate limit headers with detailed info
			m.setRateLimitHeadersFromResult(w, result)
		} else {
			// Fallback to simple Allow method
			allowed, err := m.limiter.Allow(r.Context(), key)
			if err != nil {
				if m.logger != nil {
					m.logger.Printf("Rate limiter error for key %s: %v", key, err)
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !allowed {
				m.onLimitFunc(w, r, "60")
				return
			}
		}

		// Continue to next handler
		next.ServeHTTP(w, r)
	})
}

// setRateLimitHeaders sets standard rate limit headers
func (m *RateLimitMiddleware) setRateLimitHeaders(w http.ResponseWriter, r *http.Request, key string) {
	// Try to get detailed information
	if limiterWithInfo, ok := m.limiter.(interface {
		AllowWithInfo(context.Context, string) (*ratelimiter.Result, error)
	}); ok {
		// We need to call this again to get fresh info, but don't consume a token
		// This is a limitation - ideally we'd get this from the Allow call
		result, err := limiterWithInfo.AllowWithInfo(r.Context(), key)
		if err == nil && result != nil {
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
			if !result.ResetAt.IsZero() {
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))
			}
		}
	}
}

// setRateLimitHeadersFromResult sets headers from an existing result
func (m *RateLimitMiddleware) setRateLimitHeadersFromResult(w http.ResponseWriter, result *ratelimiter.Result) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
	if !result.ResetAt.IsZero() {
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))
	}
}

// IPKeyExtractor extracts the client IP address as the rate limit key
func IPKeyExtractor(r *http.Request) string {
	// Check X-Forwarded-For header first
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP in the list
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// APIKeyExtractor extracts the API key from the request header
func APIKeyExtractor(headerName string) KeyExtractor {
	return func(r *http.Request) string {
		apiKey := r.Header.Get(headerName)
		if apiKey == "" {
			// Fallback to IP if no API key
			return IPKeyExtractor(r)
		}
		return apiKey
	}
}

// PathKeyExtractor extracts the request path as the rate limit key
func PathKeyExtractor(r *http.Request) string {
	return r.URL.Path
}

// CompositeKeyExtractor combines multiple key extractors
func CompositeKeyExtractor(extractors ...KeyExtractor) KeyExtractor {
	return func(r *http.Request) string {
		var parts []string
		for _, extractor := range extractors {
			parts = append(parts, extractor(r))
		}
		return strings.Join(parts, ":")
	}
}

// UserKeyExtractor extracts the user ID from the request context
// Assumes the user ID is stored in the context with key "userID"
func UserKeyExtractor(r *http.Request) string {
	userID := r.Context().Value("userID")
	if userID == nil {
		// Fallback to IP if no user ID
		return IPKeyExtractor(r)
	}
	return fmt.Sprintf("user:%v", userID)
}

// DefaultOnLimit is the default handler for rate-limited requests
func DefaultOnLimit(w http.ResponseWriter, r *http.Request, retryAfter string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", retryAfter)
	w.Header().Set("X-RateLimit-Limit", "")
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.WriteHeader(http.StatusTooManyRequests)
	fmt.Fprintf(w, `{"error":"rate limit exceeded","retry_after":%s}`, retryAfter)
}

// PerEndpointLimiter creates a middleware that applies different limits per endpoint
type PerEndpointLimiter struct {
	limiters map[string]ratelimiter.RateLimiter
	default_ ratelimiter.RateLimiter
}

// NewPerEndpointLimiter creates a new per-endpoint rate limiter
func NewPerEndpointLimiter(defaultLimiter ratelimiter.RateLimiter) *PerEndpointLimiter {
	return &PerEndpointLimiter{
		limiters: make(map[string]ratelimiter.RateLimiter),
		default_: defaultLimiter,
	}
}

// AddEndpoint adds a rate limiter for a specific endpoint
func (p *PerEndpointLimiter) AddEndpoint(path string, limiter ratelimiter.RateLimiter) {
	p.limiters[path] = limiter
}

// Handler returns an HTTP middleware handler
func (p *PerEndpointLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get limiter for this endpoint
		limiter, ok := p.limiters[r.URL.Path]
		if !ok {
			limiter = p.default_
		}

		// Create middleware for this request
		middleware := New(&Config{
			Limiter:      limiter,
			KeyExtractor: IPKeyExtractor,
		})

		// Handle request
		middleware.Handler(next).ServeHTTP(w, r)
	})
}
