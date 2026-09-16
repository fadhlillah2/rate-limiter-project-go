package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/middleware"
	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

func main() {
	fmt.Println("Starting Rate Limiter HTTP Middleware Example...")

	// Create a fixed window rate limiter (5 requests per 10 seconds)
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
			WindowSize:        10 * time.Second,
		},
	})
	defer limiter.Close()

	// Example 1: IP-based rate limiting
	ipRateLimiter := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.IPKeyExtractor,
		Logger:       log.Default(),
	})

	http.Handle("/api/public", ipRateLimiter.Handler(http.HandlerFunc(publicHandler)))

	// Example 2: API Key-based rate limiting
	apiKeyLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Minute,
		},
	})
	defer apiKeyLimiter.Close()

	apiKeyRateLimiter := middleware.New(&middleware.Config{
		Limiter:      apiKeyLimiter,
		KeyExtractor: middleware.APIKeyExtractor("X-API-Key"),
		Logger:       log.Default(),
	})

	http.Handle("/api/protected", apiKeyRateLimiter.Handler(http.HandlerFunc(protectedHandler)))

	// Example 3: Per-endpoint rate limiting
	perEndpointLimiter := setupPerEndpointRateLimiting()
	http.Handle("/api/strict", perEndpointLimiter.Handler(http.HandlerFunc(strictHandler)))
	http.Handle("/api/relaxed", perEndpointLimiter.Handler(http.HandlerFunc(relaxedHandler)))

	// Example 4: Custom error handler
	customErrorLimiter := setupCustomErrorHandling()
	http.Handle("/api/custom", customErrorLimiter.Handler(http.HandlerFunc(customHandler)))

	// Info endpoint
	http.HandleFunc("/", infoHandler)

	// Start server
	port := ":8080"
	fmt.Printf("Server listening on http://localhost%s\n\n", port)
	fmt.Println("Try these endpoints:")
	fmt.Println("  - http://localhost:8080/            (Info)")
	fmt.Println("  - http://localhost:8080/api/public  (IP-based limit: 5 req/10s)")
	fmt.Println("  - http://localhost:8080/api/protected (API key limit: 10 req/1m)")
	fmt.Println("  - http://localhost:8080/api/strict  (Strict limit: 2 req/10s)")
	fmt.Println("  - http://localhost:8080/api/relaxed (Relaxed limit: 20 req/10s)")
	fmt.Println("  - http://localhost:8080/api/custom  (Custom error handling)")
	fmt.Println("\nTest with:")
	fmt.Println("  curl http://localhost:8080/api/public")
	fmt.Println("  curl -H 'X-API-Key: my-key' http://localhost:8080/api/protected")
	fmt.Println()

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":   "Public endpoint accessed",
		"timestamp": time.Now().Format(time.RFC3339),
		"ip":        middleware.IPKeyExtractor(r),
	}
	jsonResponse(w, http.StatusOK, response)
}

func protectedHandler(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	response := map[string]interface{}{
		"message":   "Protected endpoint accessed",
		"timestamp": time.Now().Format(time.RFC3339),
		"api_key":   apiKey,
	}
	jsonResponse(w, http.StatusOK, response)
}

func strictHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":   "Strict endpoint (2 requests per 10 seconds)",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	jsonResponse(w, http.StatusOK, response)
}

func relaxedHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":   "Relaxed endpoint (20 requests per 10 seconds)",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	jsonResponse(w, http.StatusOK, response)
}

func customHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message":   "Custom error handling endpoint",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	jsonResponse(w, http.StatusOK, response)
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"service": "Rate Limiter HTTP Middleware Example",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"/":              "This info page",
			"/api/public":    "IP-based rate limiting (5 req/10s)",
			"/api/protected": "API key rate limiting (10 req/1m)",
			"/api/strict":    "Strict rate limit (2 req/10s)",
			"/api/relaxed":   "Relaxed rate limit (20 req/10s)",
			"/api/custom":    "Custom error handling (5 req/10s)",
		},
		"headers": map[string]string{
			"X-RateLimit-Limit":     "Maximum requests allowed",
			"X-RateLimit-Remaining": "Requests remaining in window",
			"X-RateLimit-Reset":     "Unix timestamp of window reset",
			"Retry-After":           "Seconds to wait before retry (when limited)",
		},
	}
	jsonResponse(w, http.StatusOK, info)
}

func setupPerEndpointRateLimiting() *middleware.PerEndpointLimiter {
	// Default limiter (moderate)
	defaultLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 10,
			WindowSize:        10 * time.Second,
		},
	})

	// Strict limiter
	strictLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 2,
			WindowSize:        10 * time.Second,
		},
	})

	// Relaxed limiter
	relaxedLimiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 20,
			WindowSize:        10 * time.Second,
		},
	})

	perEndpoint := middleware.NewPerEndpointLimiter(defaultLimiter)
	perEndpoint.AddEndpoint("/api/strict", strictLimiter)
	perEndpoint.AddEndpoint("/api/relaxed", relaxedLimiter)

	return perEndpoint
}

func setupCustomErrorHandling() *middleware.RateLimitMiddleware {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
			WindowSize:        10 * time.Second,
		},
	})

	return middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.IPKeyExtractor,
		OnLimit: func(w http.ResponseWriter, r *http.Request, retryAfter string) {
			// Custom error response
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)

			response := map[string]interface{}{
				"error": map[string]interface{}{
					"code":        "RATE_LIMIT_EXCEEDED",
					"message":     "You have exceeded the rate limit. Please slow down!",
					"retry_after": retryAfter,
					"timestamp":   time.Now().Format(time.RFC3339),
				},
				"help": "Wait for the specified retry_after duration before making another request",
			}

			json.NewEncoder(w).Encode(response)
		},
		Logger: log.Default(),
	})
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Example with context-based user ID
func setupUserBasedRateLimiting() http.Handler {
	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 50,
			WindowSize:        time.Minute,
		},
	})

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      limiter,
		KeyExtractor: middleware.UserKeyExtractor,
	})

	// You would wrap this with an auth middleware that sets userID in context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Example: Extract user ID from JWT token and set in context
		// userID := extractUserIDFromToken(r)
		// ctx := context.WithValue(r.Context(), "userID", userID)
		// r = r.WithContext(ctx)

		w.Write([]byte("User-specific rate limited endpoint"))
	})

	return rateLimitMiddleware.Handler(handler)
}

// Example with composite key (API key + endpoint)
func setupCompositeKeyRateLimiting() http.Handler {
	limiter := ratelimiter.NewSlidingWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	})

	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter: limiter,
		KeyExtractor: middleware.CompositeKeyExtractor(
			middleware.APIKeyExtractor("X-API-Key"),
			middleware.PathKeyExtractor,
		),
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Composite key rate limiting (API key + path)"))
	})

	return rateLimitMiddleware.Handler(handler)
}

// Example testing helper
func testRateLimit(url string, count int) {
	fmt.Printf("\nTesting %s with %d requests:\n", url, count)

	for i := 1; i <= count; i++ {
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("Request %d: Error - %v\n", i, err)
			continue
		}
		defer resp.Body.Close()

		limit := resp.Header.Get("X-RateLimit-Limit")
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		retryAfter := resp.Header.Get("Retry-After")

		if resp.StatusCode == http.StatusOK {
			fmt.Printf("Request %d: ✓ OK (Limit: %s, Remaining: %s)\n", i, limit, remaining)
		} else if resp.StatusCode == http.StatusTooManyRequests {
			fmt.Printf("Request %d: ✗ Rate Limited (Retry after: %s seconds)\n", i, retryAfter)
		} else {
			fmt.Printf("Request %d: Unexpected status %d\n", i, resp.StatusCode)
		}

		time.Sleep(100 * time.Millisecond)
	}
}
