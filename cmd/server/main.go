package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fadhlillah2/rate-limiter-project-go/pkg/middleware"
	"github.com/fadhlillah2/rate-limiter-project-go/pkg/ratelimiter"
)

type Server struct {
	httpServer *http.Server
	limiter    ratelimiter.RateLimiter
	logger     *log.Logger
}

type Response struct {
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

type ErrorResponse struct {
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// Parse command line flags
	var (
		port       = flag.String("port", "8080", "Server port")
		algorithm  = flag.String("algorithm", "fixed_window", "Rate limiting algorithm (fixed_window, sliding_window, token_bucket)")
		limit      = flag.Int("limit", 10, "Request limit per window")
		windowSize = flag.Duration("window", time.Minute, "Rate limit window size")
		redisAddr  = flag.String("redis", "", "Redis address for distributed rate limiting (optional)")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "[RATE-LIMITER] ", log.LstdFlags|log.Lshortfile)

	// Create rate limiter based on algorithm
	var limiter ratelimiter.RateLimiter
	var err error

	config := &ratelimiter.Config{
		Algorithm: ratelimiter.Algorithm(*algorithm),
		DefaultLimit: &ratelimiter.Limit{
			RequestsPerWindow: *limit,
			WindowSize:        *windowSize,
			BurstSize:         *limit * 2, // Allow 2x burst for token bucket
		},
		EnableLogging: true,
	}

	if *redisAddr != "" {
		// Use Redis-based distributed rate limiter
		redisConfig := &ratelimiter.RedisConfig{
			Addresses: []string{*redisAddr},
			PoolSize:  10,
		}
		limiter, err = ratelimiter.NewRedisLimiter(redisConfig, config)
		if err != nil {
			logger.Fatalf("Failed to create Redis limiter: %v", err)
		}
		logger.Printf("Using Redis distributed rate limiter at %s", *redisAddr)
	} else {
		// Use local in-memory rate limiter
		switch ratelimiter.Algorithm(*algorithm) {
		case ratelimiter.FixedWindow:
			limiter = ratelimiter.NewFixedWindowLimiter(config)
			logger.Printf("Using Fixed Window algorithm")
		case ratelimiter.SlidingWindowLog:
			limiter = ratelimiter.NewSlidingWindowLimiter(config)
			logger.Printf("Using Sliding Window Log algorithm")
		case ratelimiter.TokenBucket:
			limiter = ratelimiter.NewTokenBucketLimiter(config)
			logger.Printf("Using Token Bucket algorithm")
		default:
			logger.Fatalf("Unknown algorithm: %s", *algorithm)
		}
	}

	logger.Printf("Rate limit: %d requests per %s", *limit, *windowSize)

	// Create server
	server := NewServer(limiter, logger)

	// Start server
	go func() {
		logger.Printf("Starting server on port %s", *port)
		if err := server.Start(*port); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Server forced to shutdown: %v", err)
	}

	logger.Println("Server exited")
}

func NewServer(limiter ratelimiter.RateLimiter, logger *log.Logger) *Server {
	return &Server{
		limiter: limiter,
		logger:  logger,
	}
}

func (s *Server) Start(port string) error {
	mux := http.NewServeMux()

	// Create rate limit middleware
	rateLimitMiddleware := middleware.New(&middleware.Config{
		Limiter:      s.limiter,
		KeyExtractor: middleware.IPKeyExtractor,
		Logger:       s.logger,
	})

	// Public endpoints with rate limiting
	mux.Handle("/api/public", rateLimitMiddleware.Handler(http.HandlerFunc(s.handlePublic)))
	mux.Handle("/api/data", rateLimitMiddleware.Handler(http.HandlerFunc(s.handleData)))

	// API key protected endpoint with custom rate limiting
	apiKeyMiddleware := middleware.New(&middleware.Config{
		Limiter:      s.limiter,
		KeyExtractor: middleware.APIKeyExtractor("X-API-Key"),
		Logger:       s.logger,
	})
	mux.Handle("/api/protected", apiKeyMiddleware.Handler(http.HandlerFunc(s.handleProtected)))

	// Admin endpoints (no rate limiting for demo)
	mux.HandleFunc("/api/admin/limits", s.handleSetLimit)
	mux.HandleFunc("/api/admin/reset", s.handleReset)

	// Health check (no rate limiting)
	mux.HandleFunc("/health", s.handleHealth)

	// Info endpoint (no rate limiting)
	mux.HandleFunc("/", s.handleInfo)

	s.httpServer = &http.Server{
		Addr:         ":" + port,
		Handler:      s.loggingMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.limiter.Close()
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.logger.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		s.logger.Printf("Completed in %v", time.Since(start))
	})
}

// Handlers

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"service": "Rate Limiter Demo API",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"GET /":                    "This info page",
			"GET /health":              "Health check",
			"GET /api/public":          "Public endpoint (rate limited by IP)",
			"GET /api/data":            "Data endpoint (rate limited by IP)",
			"GET /api/protected":       "Protected endpoint (rate limited by API key)",
			"POST /api/admin/limits":   "Set custom rate limit for a key",
			"POST /api/admin/reset":    "Reset rate limit for a key",
		},
		"examples": map[string]string{
			"public_request":       "curl http://localhost:8080/api/public",
			"protected_request":    "curl -H 'X-API-Key: your-key' http://localhost:8080/api/protected",
			"set_limit":            `curl -X POST -H 'Content-Type: application/json' -d '{"key":"user123","limit":50,"window":"1m"}' http://localhost:8080/api/admin/limits`,
			"reset_limit":          `curl -X POST -H 'Content-Type: application/json' -d '{"key":"user123"}' http://localhost:8080/api/admin/reset`,
		},
	}
	s.jsonResponse(w, http.StatusOK, info)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status": "healthy",
		"uptime": time.Now().Unix(),
	}
	s.jsonResponse(w, http.StatusOK, health)
}

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	response := Response{
		Message:   "This is a public endpoint with rate limiting",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"ip": middleware.IPKeyExtractor(r),
		},
	}
	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	response := Response{
		Message:   "Here is some data",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"items": []string{"item1", "item2", "item3"},
			"count": 3,
		},
	}
	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleProtected(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	response := Response{
		Message:   "This is a protected endpoint",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"api_key": apiKey,
			"access":  "granted",
		},
	}
	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleSetLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Key    string `json:"key"`
		Limit  int    `json:"limit"`
		Window string `json:"window"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Key == "" {
		s.errorResponse(w, http.StatusBadRequest, "Key is required")
		return
	}

	windowDuration, err := time.ParseDuration(req.Window)
	if err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid window duration")
		return
	}

	limit := ratelimiter.NewLimit(req.Limit, windowDuration)
	if err := s.limiter.SetLimit(req.Key, limit); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := map[string]interface{}{
		"message": "Limit set successfully",
		"key":     req.Key,
		"limit":   req.Limit,
		"window":  req.Window,
	}
	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Key string `json:"key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Key == "" {
		s.errorResponse(w, http.StatusBadRequest, "Key is required")
		return
	}

	if err := s.limiter.Reset(context.Background(), req.Key); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := map[string]interface{}{
		"message": "Rate limit reset successfully",
		"key":     req.Key,
	}
	s.jsonResponse(w, http.StatusOK, response)
}

// Helper methods

func (s *Server) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) errorResponse(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:     message,
		Timestamp: time.Now(),
	}
	s.jsonResponse(w, status, response)
}
