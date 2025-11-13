package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

func TestRateLimitMiddleware_Allow(t *testing.T) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 3,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	middleware := New(&Config{
		Limiter:      limiter,
		KeyExtractor: func(r *http.Request) string { return "test-key" },
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrappedHandler := middleware.Handler(handler)

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d should succeed, got status %d", i+1, w.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Request 4 should be rate limited, got status %d", w.Code)
	}

	// Check Retry-After header
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header should be set")
	}
}

func TestRateLimitMiddleware_IPKeyExtractor(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		expectedKey    string
	}{
		{
			name:        "Remote addr without X headers",
			remoteAddr:  "192.168.1.1:12345",
			expectedKey: "192.168.1.1",
		},
		{
			name:           "X-Forwarded-For takes precedence",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "10.0.0.1, 10.0.0.2",
			expectedKey:    "10.0.0.1",
		},
		{
			name:        "X-Real-IP when no X-Forwarded-For",
			remoteAddr:  "192.168.1.1:12345",
			xRealIP:     "10.0.0.1",
			expectedKey: "10.0.0.1",
		},
		{
			name:           "X-Forwarded-For over X-Real-IP",
			remoteAddr:     "192.168.1.1:12345",
			xForwardedFor:  "10.0.0.1",
			xRealIP:        "10.0.0.2",
			expectedKey:    "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			key := IPKeyExtractor(req)
			if key != tt.expectedKey {
				t.Errorf("Expected key %s, got %s", tt.expectedKey, key)
			}
		})
	}
}

func TestRateLimitMiddleware_APIKeyExtractor(t *testing.T) {
	extractor := APIKeyExtractor("X-API-Key")

	// Test with API key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "my-api-key-123")

	key := extractor(req)
	if key != "my-api-key-123" {
		t.Errorf("Expected key 'my-api-key-123', got '%s'", key)
	}

	// Test without API key (should fallback to IP)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"

	key2 := extractor(req2)
	if key2 != "192.168.1.1" {
		t.Errorf("Expected fallback to IP '192.168.1.1', got '%s'", key2)
	}
}

func TestRateLimitMiddleware_PathKeyExtractor(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/users/123", nil)

	key := PathKeyExtractor(req)
	if key != "/api/users/123" {
		t.Errorf("Expected key '/api/users/123', got '%s'", key)
	}
}

func TestRateLimitMiddleware_CompositeKeyExtractor(t *testing.T) {
	extractor := CompositeKeyExtractor(
		func(r *http.Request) string { return "api" },
		PathKeyExtractor,
		IPKeyExtractor,
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	key := extractor(req)
	expected := "api:/test:192.168.1.1"
	if key != expected {
		t.Errorf("Expected key '%s', got '%s'", expected, key)
	}
}

func TestRateLimitMiddleware_UserKeyExtractor(t *testing.T) {
	// Test with user ID in context
	req := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(req.Context(), "userID", "user123")
	req = req.WithContext(ctx)

	key := UserKeyExtractor(req)
	if key != "user:user123" {
		t.Errorf("Expected key 'user:user123', got '%s'", key)
	}

	// Test without user ID (should fallback to IP)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"

	key2 := UserKeyExtractor(req2)
	if key2 != "192.168.1.1" {
		t.Errorf("Expected fallback to IP '192.168.1.1', got '%s'", key2)
	}
}

func TestRateLimitMiddleware_CustomOnLimit(t *testing.T) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 1,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	customCalled := false
	middleware := New(&Config{
		Limiter:      limiter,
		KeyExtractor: func(r *http.Request) string { return "test-key" },
		OnLimit: func(w http.ResponseWriter, r *http.Request, retryAfter string) {
			customCalled = true
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Custom rate limit message"))
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware.Handler(handler)

	// Use up the limit
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w1, req1)

	// Trigger rate limit with custom handler
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w2, req2)

	if !customCalled {
		t.Error("Custom OnLimit handler should be called")
	}
	if w2.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w2.Code)
	}
	if w2.Body.String() != "Custom rate limit message" {
		t.Errorf("Expected custom message, got '%s'", w2.Body.String())
	}
}

func TestPerEndpointLimiter(t *testing.T) {
	defaultLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
		},
	})
	defer defaultLimiter.Close()

	strictLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Second,
		},
	})
	defer strictLimiter.Close()

	perEndpoint := NewPerEndpointLimiter(defaultLimiter)
	perEndpoint.AddEndpoint("/api/strict", strictLimiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := perEndpoint.Handler(handler)

	// Test strict endpoint (limit of 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/strict", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Strict endpoint request %d should succeed", i+1)
		}
	}

	// 3rd request to strict endpoint should be rate limited
	req := httptest.NewRequest("GET", "/api/strict", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Strict endpoint request 3 should be rate limited")
	}

	// Default endpoint should use default limit (10)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/default", nil)
		req.RemoteAddr = "192.168.1.2:12345"
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Default endpoint request %d should succeed", i+1)
		}
	}
}

func TestDefaultOnLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	DefaultOnLimit(w, req, "60")

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Expected Retry-After '60', got '%s'", retryAfter)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}
