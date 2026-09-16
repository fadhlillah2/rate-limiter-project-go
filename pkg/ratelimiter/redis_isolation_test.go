package ratelimiter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisNamespaceIsolation(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "sliding", true: "fixed"}[fixed], func(t *testing.T) {
			mr := miniredis.RunT(t)
			newLimiter := func(prefix string) *RedisLimiter {
				limiter, err := NewRedisLimiter(&RedisConfig{Addresses: []string{mr.Addr()}, KeyPrefix: prefix}, &Config{DefaultLimit: NewLimit(1, time.Hour)})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { limiter.Close() })
				return limiter
			}
			a, b := newLimiter("app-a:"), newLimiter("app-b:")
			allow := func(limiter *RedisLimiter) bool {
				var result *Result
				var err error
				if fixed {
					result, err = limiter.AllowWithFixedWindow(context.Background(), "user")
				} else {
					result, err = limiter.AllowWithInfo(context.Background(), "user")
				}
				if err != nil {
					t.Fatal(err)
				}
				return result.Allowed
			}
			if !allow(a) || !allow(b) {
				t.Fatal("different configured prefixes share quota")
			}
			if err := a.Reset(context.Background(), "user"); err != nil {
				t.Fatal(err)
			}
			if allow(b) {
				t.Fatal("reset crossed configured prefix")
			}
			if !allow(a) {
				t.Fatal("own quota was not reset")
			}
		})
	}
}

func TestRedisResetExactLogicalKey(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		for _, pair := range [][2]string{{"a", "ab"}, {"a*", "ab"}, {"a?", "ab"}, {"a[bc]", "ab"}, {"a", "a:123"}} {
			t.Run(map[bool]string{false: "sliding/", true: "fixed/"}[fixed]+pair[0]+"/"+pair[1], func(t *testing.T) {
				mr, limiter := setupTestRedis(t)
				defer mr.Close()
				defer limiter.Close()
				ctx := context.Background()
				allow := func(key string) bool {
					var result *Result
					var err error
					if fixed {
						result, err = limiter.AllowWithFixedWindow(ctx, key)
					} else {
						result, err = limiter.AllowWithInfo(ctx, key)
					}
					if err != nil {
						t.Fatal(err)
					}
					return result.Allowed
				}
				for _, key := range pair {
					if err := limiter.SetLimit(key, NewLimit(1, time.Hour)); err != nil {
						t.Fatal(err)
					}
					if !allow(key) {
						t.Fatal("fresh key denied")
					}
				}
				if err := limiter.Reset(ctx, pair[0]); err != nil {
					t.Fatal(err)
				}
				if allow(pair[1]) {
					t.Error("reset removed sibling quota")
				}
				if !allow(pair[0]) {
					t.Error("reset did not clear exact logical key")
				}
			})
		}
	}
}

func TestRedisResetNumericSuffixAcrossAlgorithms(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()
	ctx := context.Background()
	// Derive the adversarial logical key from the legacy fixed-window format.
	window := 100 * 365 * 24 * time.Hour
	sibling := fmt.Sprintf("a:%d", time.Now().UnixMilli()/window.Milliseconds())
	for _, key := range []string{"a", sibling} {
		if err := limiter.SetLimit(key, NewLimit(1, window)); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := limiter.AllowWithFixedWindow(ctx, "a"); err != nil || !result.Allowed {
		t.Fatalf("fixed request: result=%+v err=%v", result, err)
	}
	if result, err := limiter.AllowWithInfo(ctx, sibling); err != nil || !result.Allowed {
		t.Fatalf("distinct sliding logical key collided with fixed window: result=%+v err=%v", result, err)
	}
	if err := limiter.Reset(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if result, err := limiter.AllowWithInfo(ctx, sibling); err != nil || result.Allowed {
		t.Fatalf("reset affected distinct sliding key: result=%+v err=%v", result, err)
	}
}

func TestRedisLegacyKeysUntouched(t *testing.T) {
	mr, limiter := setupTestRedis(t)
	defer mr.Close()
	defer limiter.Close()
	// The second key would alias a proposed new root nested under legacy ratelimit:.
	legacyKeys := []string{"ratelimit:a", "ratelimit:v2:726174656c696d69743a:61:sliding"}
	for _, key := range legacyKeys {
		if err := mr.Set(key, "legacy-value"); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if result, err := limiter.AllowWithInfo(ctx, "a"); err != nil || !result.Allowed {
		t.Fatalf("legacy state consumed: %+v, %v", result, err)
	}
	if result, err := limiter.AllowWithFixedWindow(ctx, "a"); err != nil || !result.Allowed {
		t.Fatalf("algorithms share state: %+v, %v", result, err)
	}
	if err := limiter.Reset(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	for _, key := range legacyKeys {
		if value, err := mr.Get(key); err != nil || value != "legacy-value" {
			t.Fatalf("legacy key %q changed: %q, %v", key, value, err)
		}
	}
	if keys := mr.Keys(); len(keys) != len(legacyKeys) {
		t.Fatalf("reset left own counters: %v", keys)
	}
}

func TestRedisPrefixComponentBoundaries(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	newLimiter := func(prefix string) *RedisLimiter {
		limiter, err := NewRedisLimiter(&RedisConfig{Addresses: []string{mr.Addr()}, KeyPrefix: prefix}, &Config{DefaultLimit: NewLimit(1, time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { limiter.Close() })
		return limiter
	}
	// Prefix/key concatenation and Redis glob syntax must not alias namespaces.
	a, b := newLimiter("app:*?:"), newLimiter("app:*?:x:")
	for _, entry := range []struct {
		limiter *RedisLimiter
		key     string
	}{{a, "x:user"}, {b, "user"}} {
		if ok, err := entry.limiter.Allow(ctx, entry.key); err != nil || !ok {
			t.Fatalf("namespace collision: %v, %v", ok, err)
		}
	}
	if err := a.Reset(ctx, "x:user"); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Allow(ctx, "user"); err != nil || ok {
		t.Fatalf("reset crossed component boundary: %v, %v", ok, err)
	}
	implicit, explicit := newLimiter(""), newLimiter("ratelimit:")
	if ok, err := implicit.Allow(ctx, "default"); err != nil || !ok {
		t.Fatal("default request failed", err)
	}
	if ok, err := explicit.Allow(ctx, "default"); err != nil || ok {
		t.Fatal("explicit default namespace must share quota", err)
	}
}
