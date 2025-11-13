package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

func main() {
	fmt.Println("=== Rate Limiter Basic Usage Examples ===\n")

	// Example 1: Fixed Window Rate Limiter
	example1FixedWindow()

	// Example 2: Sliding Window Rate Limiter
	example2SlidingWindow()

	// Example 3: Token Bucket Rate Limiter
	example3TokenBucket()

	// Example 4: Custom Limits per Key
	example4CustomLimits()

	// Example 5: Concurrent Usage
	example5Concurrent()
}

func example1FixedWindow() {
	fmt.Println("Example 1: Fixed Window Rate Limiter")
	fmt.Println("=====================================")

	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Second * 3,
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	key := "user123"

	// Make 7 requests
	for i := 1; i <= 7; i++ {
		allowed, err := limiter.Allow(ctx, key)
		if err != nil {
			log.Printf("Error: %v\n", err)
			continue
		}

		if allowed {
			fmt.Printf("Request %d: ✓ Allowed\n", i)
		} else {
			fmt.Printf("Request %d: ✗ Rate limited\n", i)
		}
	}

	// Wait for window to reset
	fmt.Println("\nWaiting for window to reset (3 seconds)...")
	time.Sleep(3 * time.Second)

	// Try again
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		fmt.Println("Request after reset: ✓ Allowed\n")
	}
}

func example2SlidingWindow() {
	fmt.Println("\nExample 2: Sliding Window Rate Limiter")
	fmt.Println("=======================================")

	limiter := ratelimiter.NewSlidingWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 3,
			WindowSize:        time.Second * 2,
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	key := "user456"

	// Make requests with delays
	for i := 1; i <= 5; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if allowed {
			fmt.Printf("Request %d: ✓ Allowed\n", i)
		} else {
			fmt.Printf("Request %d: ✗ Rate limited\n", i)
		}

		if i == 3 {
			fmt.Println("Waiting 1 second...")
			time.Sleep(time.Second)
		}
	}
	fmt.Println()
}

func example3TokenBucket() {
	fmt.Println("\nExample 3: Token Bucket Rate Limiter")
	fmt.Println("=====================================")

	limiter := ratelimiter.NewTokenBucketLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 10,  // 10 tokens per second
			WindowSize:        time.Second,
			BurstSize:         5,   // Bucket capacity
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	key := "user789"

	// Test burst
	fmt.Println("Testing burst (up to 5 requests):")
	for i := 1; i <= 7; i++ {
		allowed, _ := limiter.Allow(ctx, key)
		if allowed {
			fmt.Printf("Burst request %d: ✓ Allowed\n", i)
		} else {
			fmt.Printf("Burst request %d: ✗ Rate limited\n", i)
		}
	}

	// Wait for token refill
	fmt.Println("\nWaiting for tokens to refill (500ms)...")
	time.Sleep(500 * time.Millisecond)

	// Should have ~5 tokens now
	allowed, _ := limiter.Allow(ctx, key)
	if allowed {
		fmt.Println("After refill: ✓ Allowed\n")
	}
}

func example4CustomLimits() {
	fmt.Println("\nExample 4: Custom Limits per Key")
	fmt.Println("=================================")

	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 5,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	// Set custom limit for premium user
	premiumKey := "premium-user"
	limiter.SetLimit(premiumKey, &ratelimiter.Limit{
		RequestsPerWindow: 100,
		WindowSize:        time.Minute,
	})

	// Set custom limit for free user
	freeKey := "free-user"
	limiter.SetLimit(freeKey, &ratelimiter.Limit{
		RequestsPerWindow: 3,
		WindowSize:        time.Minute,
	})

	fmt.Printf("Premium user limit: %d requests/min\n",
		limiter.GetLimit(premiumKey).RequestsPerWindow)
	fmt.Printf("Free user limit: %d requests/min\n",
		limiter.GetLimit(freeKey).RequestsPerWindow)

	// Test free user limit
	for i := 1; i <= 4; i++ {
		allowed, _ := limiter.Allow(ctx, freeKey)
		if allowed {
			fmt.Printf("Free user request %d: ✓ Allowed\n", i)
		} else {
			fmt.Printf("Free user request %d: ✗ Rate limited\n", i)
		}
	}
	fmt.Println()
}

func example5Concurrent() {
	fmt.Println("\nExample 5: Concurrent Usage")
	fmt.Println("===========================")

	limiter := ratelimiter.NewFixedWindowLimiter(&ratelimiter.Config{
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: 10,
			WindowSize:        time.Second,
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	key := "concurrent-user"

	// Simulate 20 concurrent requests
	results := make(chan bool, 20)

	for i := 0; i < 20; i++ {
		go func(id int) {
			allowed, _ := limiter.Allow(ctx, key)
			results <- allowed
		}(i)
	}

	// Collect results
	allowedCount := 0
	deniedCount := 0

	for i := 0; i < 20; i++ {
		if <-results {
			allowedCount++
		} else {
			deniedCount++
		}
	}

	fmt.Printf("Concurrent requests: 20\n")
	fmt.Printf("Allowed: %d\n", allowedCount)
	fmt.Printf("Denied: %d\n", deniedCount)
	fmt.Printf("Expected: 10 allowed, 10 denied\n")

	if allowedCount == 10 && deniedCount == 10 {
		fmt.Println("✓ Thread-safe concurrent handling verified!\n")
	} else {
		fmt.Println("✗ Unexpected result\n")
	}
}
