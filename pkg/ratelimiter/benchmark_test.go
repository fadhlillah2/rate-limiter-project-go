package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

func BenchmarkFixedWindow_Allow(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

func BenchmarkFixedWindow_AllowParallel(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			limiter.Allow(ctx, "bench-key")
			i++
		}
	})
}

func BenchmarkFixedWindow_MultipleKeys(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	keys := []string{"key1", "key2", "key3", "key4", "key5"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]
		limiter.Allow(ctx, key)
	}
}

func BenchmarkSlidingWindow_Allow(b *testing.B) {
	limiter := NewSlidingWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

func BenchmarkSlidingWindow_AllowParallel(b *testing.B) {
	limiter := NewSlidingWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			limiter.Allow(ctx, "bench-key")
			i++
		}
	})
}

func BenchmarkTokenBucket_Allow(b *testing.B) {
	limiter := NewTokenBucketLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
			BurstSize:         1000000,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

func BenchmarkTokenBucket_AllowParallel(b *testing.B) {
	limiter := NewTokenBucketLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
			BurstSize:         1000000,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			limiter.Allow(ctx, "bench-key")
			i++
		}
	})
}

func BenchmarkFixedWindow_SetLimit(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	limit := &Limit{
		RequestsPerWindow: 200,
		WindowSize:        time.Minute,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.SetLimit("key", limit)
	}
}

func BenchmarkFixedWindow_GetLimit(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 100,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.GetLimit("key")
	}
}

func BenchmarkFixedWindow_AllowWithInfo(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.AllowWithInfo(ctx, "bench-key")
	}
}

func BenchmarkSlidingWindow_AllowWithInfo(b *testing.B) {
	limiter := NewSlidingWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.AllowWithInfo(ctx, "bench-key")
	}
}

func BenchmarkTokenBucket_AllowWithInfo(b *testing.B) {
	limiter := NewTokenBucketLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
			BurstSize:         1000000,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.AllowWithInfo(ctx, "bench-key")
	}
}

// Benchmark concurrent scenarios
func BenchmarkConcurrent_FixedWindow_1000Goroutines(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ResetTimer()
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Allow(ctx, "bench-key")
		}()
	}
	wg.Wait()
}

// Benchmark memory allocations
func BenchmarkMemory_FixedWindow(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

func BenchmarkMemory_SlidingWindow(b *testing.B) {
	limiter := NewSlidingWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

func BenchmarkMemory_TokenBucket(b *testing.B) {
	limiter := NewTokenBucketLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
			BurstSize:         1000000,
		},
	})
	defer limiter.Close()

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "bench-key")
	}
}

// Comparison benchmarks
func BenchmarkComparison_Allow(b *testing.B) {
	algorithms := map[string]RateLimiter{
		"FixedWindow": NewFixedWindowLimiter(&Config{
			DefaultLimit: &Limit{RequestsPerWindow: 1000000, WindowSize: time.Minute},
		}),
		"SlidingWindow": NewSlidingWindowLimiter(&Config{
			DefaultLimit: &Limit{RequestsPerWindow: 1000000, WindowSize: time.Minute},
		}),
		"TokenBucket": NewTokenBucketLimiter(&Config{
			DefaultLimit: &Limit{RequestsPerWindow: 1000000, WindowSize: time.Minute, BurstSize: 1000000},
		}),
	}

	ctx := context.Background()

	for name, limiter := range algorithms {
		b.Run(name, func(b *testing.B) {
			defer limiter.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				limiter.Allow(ctx, "bench-key")
			}
		})
	}
}

// Real-world scenario benchmark
func BenchmarkRealWorld_MixedOperations(b *testing.B) {
	limiter := NewFixedWindowLimiter(&Config{
		DefaultLimit: &Limit{
			RequestsPerWindow: 1000000,
			WindowSize:        time.Minute,
		},
	})
	defer limiter.Close()

	ctx := context.Background()
	keys := []string{"user1", "user2", "user3", "user4", "user5"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]

		// 80% Allow, 10% GetLimit, 10% SetLimit
		switch i % 10 {
		case 0:
			limiter.SetLimit(key, &Limit{RequestsPerWindow: 100, WindowSize: time.Minute})
		case 1:
			limiter.GetLimit(key)
		default:
			limiter.Allow(ctx, key)
		}
	}
}
