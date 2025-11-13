package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/middleware"
	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

func setupTestServer() (*httptest.Server, *ratelimiter.FixedWindowLimiter) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second,
		},
	})

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.IPKeyExtractor,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "success",
		})
	})

	server := httptest.NewServer(rateLimitMiddleware.Handler(handler))
	return server, limiter
}

func TestIntegration_RateLimitHeaders(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	// First request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check rate limit headers
	limit := resp.Header.Get("X-RateLimit-Limit")
	if limit == "" {
		t.Error("X-RateLimit-Limit header should be set")
	}

	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "" {
		t.Error("X-RateLimit-Remaining header should be set")
	}

	reset := resp.Header.Get("X-RateLimit-Reset")
	if reset == "" {
		t.Error("X-RateLimit-Reset header should be set")
	}
}

func TestIntegration_RateLimitEnforcement(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	client := &http.Client{}

	// Make requests up to limit (5 requests)
	for i := 0; i < 5; i++ {
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, resp.StatusCode)
		}
	}

	// 6th request should be rate limited
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Request 6 failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Request 6: expected status 429, got %d", resp.StatusCode)
	}

	// Check Retry-After header
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header should be set when rate limited")
	}
}

func TestIntegration_RateLimitReset(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	client := &http.Client{}

	// Use up the limit
	for i := 0; i < 5; i++ {
		client.Get(server.URL)
	}

	// Should be rate limited
	resp, _ := client.Get(server.URL)
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Error("Should be rate limited")
	}

	// Wait for window to reset
	time.Sleep(1100 * time.Millisecond)

	// Should work again
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Request after reset failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("After reset: expected status 200, got %d", resp.StatusCode)
	}
}

func TestIntegration_APIKeyRateLimiting(t *testing.T) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 3,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.APIKeyExtractor("X-API-Key"),
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(rateLimitMiddleware.Handler(handler))
	defer server.Close()

	client := &http.Client{}

	// Test with API key
	apiKey := "test-key-123"
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", server.URL, nil)
		req.Header.Set("X-API-Key", apiKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, resp.StatusCode)
		}
	}

	// 4th request should be rate limited
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("X-API-Key", apiKey)

	resp, _ := client.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Request 4: expected status 429, got %d", resp.StatusCode)
	}

	// Different API key should still work
	req2, _ := http.NewRequest("GET", server.URL, nil)
	req2.Header.Set("X-API-Key", "different-key")

	resp2, _ := client.Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Different API key: expected status 200, got %d", resp2.StatusCode)
	}
}

func TestIntegration_CustomErrorHandler(t *testing.T) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 1,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	customOnLimit := func(w http.ResponseWriter, r *http.Request, retryAfter string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "rate-limited")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "custom rate limit message",
		})
	}

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.IPKeyExtractor,
		OnLimit:      customOnLimit,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(rateLimitMiddleware.Handler(handler))
	defer server.Close()

	client := &http.Client{}

	// First request succeeds
	client.Get(server.URL)

	// Second request should trigger custom error handler
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resp.StatusCode)
	}

	customHeader := resp.Header.Get("X-Custom-Header")
	if customHeader != "rate-limited" {
		t.Errorf("Expected custom header, got '%s'", customHeader)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "custom rate limit message" {
		t.Errorf("Expected custom error message, got '%s'", body["error"])
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	client := &http.Client{}
	results := make(chan int, 20)

	// Send 20 concurrent requests
	for i := 0; i < 20; i++ {
		go func() {
			resp, err := client.Get(server.URL)
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	// Collect results
	okCount := 0
	rateLimitedCount := 0

	for i := 0; i < 20; i++ {
		status := <-results
		if status == http.StatusOK {
			okCount++
		} else if status == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Should have 5 successful and 15 rate limited (limit is 5)
	if okCount != 5 {
		t.Errorf("Expected 5 successful requests, got %d", okCount)
	}
	if rateLimitedCount != 15 {
		t.Errorf("Expected 15 rate limited requests, got %d", rateLimitedCount)
	}
}

func TestIntegration_PerEndpointLimiting(t *testing.T) {
	defaultLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
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

	perEndpoint := middleware.NewPerEndpointLimiter(defaultLimiter)
	perEndpoint.AddEndpoint("/strict", strictLimiter)

	mux := http.NewServeMux()

	mux.HandleFunc("/default", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/strict", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(perEndpoint.Handler(mux))
	defer server.Close()

	client := &http.Client{}

	// Test strict endpoint (limit of 2)
	for i := 0; i < 2; i++ {
		resp, _ := client.Get(server.URL + "/strict")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Strict endpoint request %d should succeed", i+1)
		}
	}

	// 3rd request to strict endpoint should be rate limited
	resp, _ := client.Get(server.URL + "/strict")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Error("Strict endpoint request 3 should be rate limited")
	}

	// Default endpoint should still work (limit of 5)
	for i := 0; i < 5; i++ {
		resp, _ := client.Get(server.URL + "/default")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Default endpoint request %d should succeed", i+1)
		}
	}
}

func TestIntegration_JSONErrorResponse(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	client := &http.Client{}

	// Use up limit
	for i := 0; i < 5; i++ {
		client.Get(server.URL)
	}

	// Get rate limited response
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check Content-Type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected JSON content type, got '%s'", contentType)
	}

	// Parse JSON body
	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	if err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Check error field
	if body["error"] == nil {
		t.Error("JSON response should contain 'error' field")
	}
}

func TestIntegration_MultipleClients(t *testing.T) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 2,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	// Use API key as identifier
	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.APIKeyExtractor("X-Client-ID"),
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(rateLimitMiddleware.Handler(handler))
	defer server.Close()

	client := &http.Client{}

	// Client 1 makes 2 requests
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", server.URL, nil)
		req.Header.Set("X-Client-ID", "client1")
		resp, _ := client.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Client1 request %d should succeed", i+1)
		}
	}

	// Client 1's 3rd request should be rate limited
	req1, _ := http.NewRequest("GET", server.URL, nil)
	req1.Header.Set("X-Client-ID", "client1")
	resp1, _ := client.Do(req1)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusTooManyRequests {
		t.Error("Client1 should be rate limited")
	}

	// Client 2 should still have full quota
	req2, _ := http.NewRequest("GET", server.URL, nil)
	req2.Header.Set("X-Client-ID", "client2")
	resp2, _ := client.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Error("Client2 should not be rate limited")
	}
}

func TestIntegration_ResponseBodyAfterRateLimit(t *testing.T) {
	server, limiter := setupTestServer()
	defer server.Close()
	defer limiter.Close()

	client := &http.Client{}

	// Success case - check response body
	resp, _ := client.Get(server.URL)
	var successBody map[string]string
	json.NewDecoder(resp.Body).Decode(&successBody)
	resp.Body.Close()

	if successBody["message"] != "success" {
		t.Errorf("Expected success message, got '%s'", successBody["message"])
	}

	// Use up limit and trigger rate limit
	for i := 0; i < 5; i++ {
		client.Get(server.URL)
	}

	// Rate limited case - should get error in body
	resp, _ = client.Get(server.URL)
	var errorBody map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&errorBody)
	resp.Body.Close()

	if errorBody["error"] == nil {
		t.Error("Rate limited response should contain error")
	}
}

func BenchmarkHTTPServer_RateLimiter(b *testing.B) {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 1000000, // High limit for benchmark
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: func(r *http.Request) string { return "benchmark" },
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(rateLimitMiddleware.Handler(handler))
	defer server.Close()

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := client.Get(server.URL)
		resp.Body.Close()
	}
}
