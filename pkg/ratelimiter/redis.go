package ratelimiter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter implements a distributed rate limiter using Redis
// It supports both fixed window and sliding window algorithms
type RedisLimiter struct {
	client       redis.UniversalClient
	config       *Config
	customLimits sync.Map // map[string]*Limit
}

// RedisConfig holds configuration for Redis connection
type RedisConfig struct {
	// Addresses for Redis cluster or single instance
	Addresses []string

	// Password for Redis authentication
	Password string

	// DB is the Redis database number (default: 0)
	DB int

	// PoolSize is the maximum number of socket connections
	PoolSize int

	// KeyPrefix is prepended to all keys
	KeyPrefix string
}

// NewRedisLimiter creates a new Redis-based distributed rate limiter
func NewRedisLimiter(redisConfig *RedisConfig, config *Config) (*RedisLimiter, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if config.DefaultLimit == nil {
		config.DefaultLimit = NewLimit(100, time.Minute)
	}

	if redisConfig == nil {
		return nil, fmt.Errorf("redis configuration is required")
	}

	var client redis.UniversalClient

	if len(redisConfig.Addresses) == 0 {
		return nil, fmt.Errorf("at least one Redis address is required")
	}

	if len(redisConfig.Addresses) == 1 {
		// Single instance
		client = redis.NewClient(&redis.Options{
			Addr:     redisConfig.Addresses[0],
			Password: redisConfig.Password,
			DB:       redisConfig.DB,
			PoolSize: redisConfig.PoolSize,
		})
	} else {
		// Cluster mode
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    redisConfig.Addresses,
			Password: redisConfig.Password,
			PoolSize: redisConfig.PoolSize,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisLimiter{
		client: client,
		config: config,
	}, nil
}

// Allow checks if a request should be allowed for the given key
func (r *RedisLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return r.AllowN(ctx, key, 1)
}

// AllowN checks if N requests should be allowed for the given key
func (r *RedisLimiter) AllowN(ctx context.Context, key string, n int) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("n must be positive, got %d", n)
	}

	result, err := r.AllowWithInfo(ctx, key)
	if err != nil {
		return false, err
	}

	return result.Allowed, nil
}

// AllowWithInfo checks if a request should be allowed using sliding window algorithm
func (r *RedisLimiter) AllowWithInfo(ctx context.Context, key string) (*Result, error) {
	limit := r.GetLimit(key)
	if limit == nil {
		return nil, fmt.Errorf("no limit configured for key: %s", key)
	}

	redisKey := r.buildKey(key)
	now := time.Now()
	windowStart := now.Add(-limit.WindowSize)

	// Use Lua script for atomic operations
	script := redis.NewScript(`
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window_start = tonumber(ARGV[2])
		local limit = tonumber(ARGV[3])
		local window_size = tonumber(ARGV[4])

		-- Remove old entries outside the window
		redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

		-- Count current entries
		local current = redis.call('ZCARD', key)

		-- Check if limit exceeded
		if current >= limit then
			local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
			local retry_after = 0
			if #oldest > 0 then
				retry_after = tonumber(oldest[2]) + window_size - now
				if retry_after < 0 then retry_after = 0 end
			end
			return {0, current, limit, retry_after}
		end

		-- Add new entry
		redis.call('ZADD', key, now, now .. ':' .. math.random())
		redis.call('EXPIRE', key, math.ceil(window_size))

		return {1, current + 1, limit, 0}
	`)

	windowSizeMs := limit.WindowSize.Milliseconds()
	nowMs := now.UnixMilli()
	windowStartMs := windowStart.UnixMilli()

	result, err := script.Run(ctx, r.client, []string{redisKey},
		nowMs,
		windowStartMs,
		limit.RequestsPerWindow,
		windowSizeMs/1000, // Convert to seconds for EXPIRE
	).Result()

	if err != nil {
		return nil, fmt.Errorf("redis script failed: %w", err)
	}

	values := result.([]interface{})
	allowed := values[0].(int64) == 1
	current := int(values[1].(int64))
	maxLimit := int(values[2].(int64))
	retryAfterMs := values[3].(int64)

	remaining := maxLimit - current
	if remaining < 0 {
		remaining = 0
	}

	return &Result{
		Allowed:    allowed,
		Limit:      maxLimit,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
		ResetAt:    now.Add(limit.WindowSize),
	}, nil
}

// AllowWithFixedWindow implements fixed window algorithm using Redis
func (r *RedisLimiter) AllowWithFixedWindow(ctx context.Context, key string) (*Result, error) {
	limit := r.GetLimit(key)
	if limit == nil {
		return nil, fmt.Errorf("no limit configured for key: %s", key)
	}

	redisKey := r.buildKey(key)
	now := time.Now()

	// Use current window timestamp as key suffix
	windowKey := fmt.Sprintf("%s:%d", redisKey, now.Unix()/int64(limit.WindowSize.Seconds()))

	script := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window_size = tonumber(ARGV[2])

		local current = tonumber(redis.call('GET', key) or "0")

		if current >= limit then
			local ttl = redis.call('TTL', key)
			return {0, current, limit, ttl}
		end

		local new_count = redis.call('INCR', key)
		if new_count == 1 then
			redis.call('EXPIRE', key, window_size)
		end

		local ttl = redis.call('TTL', key)
		return {1, new_count, limit, ttl}
	`)

	result, err := script.Run(ctx, r.client, []string{windowKey},
		limit.RequestsPerWindow,
		int(limit.WindowSize.Seconds()),
	).Result()

	if err != nil {
		return nil, fmt.Errorf("redis script failed: %w", err)
	}

	values := result.([]interface{})
	allowed := values[0].(int64) == 1
	current := int(values[1].(int64))
	maxLimit := int(values[2].(int64))
	ttl := values[3].(int64)

	remaining := maxLimit - current
	if remaining < 0 {
		remaining = 0
	}

	var retryAfter time.Duration
	if !allowed && ttl > 0 {
		retryAfter = time.Duration(ttl) * time.Second
	}

	return &Result{
		Allowed:    allowed,
		Limit:      maxLimit,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAt:    now.Add(time.Duration(ttl) * time.Second),
	}, nil
}

// Reset resets the rate limit for the given key
func (r *RedisLimiter) Reset(ctx context.Context, key string) error {
	redisKey := r.buildKey(key)

	// Delete all keys matching the pattern
	iter := r.client.Scan(ctx, 0, redisKey+"*", 0).Iterator()
	for iter.Next(ctx) {
		if err := r.client.Del(ctx, iter.Val()).Err(); err != nil {
			return fmt.Errorf("failed to delete key %s: %w", iter.Val(), err)
		}
	}

	return iter.Err()
}

// GetLimit returns the current limit configuration for a key
func (r *RedisLimiter) GetLimit(key string) *Limit {
	if customLimit, ok := r.customLimits.Load(key); ok {
		return customLimit.(*Limit)
	}
	return r.config.DefaultLimit
}

// SetLimit sets a custom limit for a specific key
func (r *RedisLimiter) SetLimit(key string, limit *Limit) error {
	if limit == nil {
		return fmt.Errorf("limit cannot be nil")
	}
	if limit.RequestsPerWindow <= 0 {
		return fmt.Errorf("requests per window must be positive")
	}
	if limit.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive")
	}

	r.customLimits.Store(key, limit)
	return nil
}

// buildKey constructs the Redis key with optional prefix
func (r *RedisLimiter) buildKey(key string) string {
	return fmt.Sprintf("ratelimit:%s", key)
}

// Close cleans up resources
func (r *RedisLimiter) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// GetClient returns the underlying Redis client for advanced usage
func (r *RedisLimiter) GetClient() redis.UniversalClient {
	return r.client
}
