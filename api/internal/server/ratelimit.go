package server

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) allow(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	var valid []time.Time
	for _, t := range rl.requests[key] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[key] = valid
		return false, rl.limit - len(valid)
	}

	valid = append(valid, now)
	rl.requests[key] = valid
	return true, rl.limit - len(valid)
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil || r.URL.Path == "/healthz" || r.URL.Path == "/healthz/live" || r.URL.Path == "/healthz/ready" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		key := extractIP(r)
		allowed, remaining := s.limiter.allow(key)

		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.limiter.limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(s.limiter.window.Seconds())))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}
