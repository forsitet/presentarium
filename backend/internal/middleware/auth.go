// Package middleware provides HTTP middleware functions.
package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"presentarium/internal/errs"
	"presentarium/internal/service"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// Auth returns a middleware that validates the JWT Bearer token and injects
// the user ID into the request context. Returns 401 if missing or invalid.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			userID, err := service.ValidateAccessToken(tokenStr, jwtSecret)
			if err != nil {
				if errs.IsUnauthorized(err) {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ipEntry tracks recent request timestamps for one IP.
type ipEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// RateLimiter holds per-IP sliding-window state.
type RateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*ipEntry
	limit    int
	window   time.Duration
	cleanAt  time.Time
}

// NewRateLimiter creates a limiter allowing limit requests per window per IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		entries: make(map[string]*ipEntry),
		limit:   limit,
		window:  window,
		cleanAt: time.Now().Add(window * 2),
	}
}

// Allow returns true if the IP is within the rate limit.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	entry, ok := rl.entries[ip]
	if !ok {
		entry = &ipEntry{}
		rl.entries[ip] = entry
	}
	// Periodic cleanup of stale IPs
	if time.Now().After(rl.cleanAt) {
		rl.cleanStale()
		rl.cleanAt = time.Now().Add(rl.window * 2)
	}
	rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Slide window: remove timestamps older than window
	valid := entry.timestamps[:0]
	for _, t := range entry.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	entry.timestamps = valid

	if len(entry.timestamps) >= rl.limit {
		return false
	}
	entry.timestamps = append(entry.timestamps, now)
	return true
}

// cleanStale removes IPs with no recent requests (caller holds rl.mu).
func (rl *RateLimiter) cleanStale() {
	cutoff := time.Now().Add(-rl.window)
	for ip, e := range rl.entries {
		e.mu.Lock()
		allOld := true
		for _, t := range e.timestamps {
			if t.After(cutoff) {
				allOld = false
				break
			}
		}
		e.mu.Unlock()
		if allOld {
			delete(rl.entries, ip)
		}
	}
}

// RateLimit returns a middleware that limits requests per IP.
// Responds with 429 Too Many Requests when exceeded.
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"too many requests"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIP extracts the client IP from common proxy headers or RemoteAddr.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.SplitN(ip, ",", 2)[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// Strip port from RemoteAddr
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
