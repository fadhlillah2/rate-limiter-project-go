# Rate Limiter Examples

This directory contains runnable examples demonstrating how to use the rate limiter library.

## Running the Examples

### Basic Usage Example

Shows how to use different rate limiting algorithms in Go code:

```bash
go run basic_usage.go
```

This example demonstrates:
- Fixed Window rate limiter
- Sliding Window rate limiter
- Token Bucket rate limiter
- Custom limits per key
- Concurrent usage (thread-safety)

### HTTP Middleware Example

Shows how to integrate rate limiting into an HTTP server:

```bash
go run http_middleware.go
```

This example demonstrates:
- IP-based rate limiting
- API key-based rate limiting
- Per-endpoint rate limiting
- Custom error handling
- Rate limit headers

Then test the endpoints with curl:

```bash
# Test IP-based rate limiting
for i in {1..10}; do curl http://localhost:8080/api/public; echo ""; done

# Test API key rate limiting
curl -H "X-API-Key: my-key" http://localhost:8080/api/protected

# Test strict endpoint (2 req/10s)
curl http://localhost:8080/api/strict

# Test relaxed endpoint (20 req/10s)
curl http://localhost:8080/api/relaxed
```

## Note

These are standalone programs, not test files. They are meant to be run directly to see the rate limiter in action.
