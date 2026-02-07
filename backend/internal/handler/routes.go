package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
)

// RouterDeps holds dependencies needed to build the router.
type RouterDeps struct {
	AuthService         service.AuthService
	PollService         service.PollService
	JWTSecret           string
	RefreshTokenTTLDays int
}

// NewRouter creates and configures the HTTP router.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	r.Get("/health", healthHandler)

	// Auth rate limiter: 5 req/min per IP
	authLimiter := appmw.NewRateLimiter(5, time.Minute)

	authH := newAuthHandler(deps.AuthService, deps.RefreshTokenTTLDays)
	pollH := newPollHandler(deps.PollService)

	r.Route("/api", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Use(appmw.RateLimit(authLimiter))
			r.Post("/register", authH.handleRegister)
			r.Post("/login", authH.handleLogin)
			r.Post("/refresh", authH.handleRefresh)
			r.Post("/logout", authH.handleLogout)
		})

		// Protected routes (JWT required)
		r.Group(func(r chi.Router) {
			r.Use(appmw.Auth(deps.JWTSecret))

			// Poll routes
			r.Route("/polls", func(r chi.Router) {
				r.Get("/", pollH.handleList)
				r.Post("/", pollH.handleCreate)
				r.Get("/{id}", pollH.handleGet)
				r.Put("/{id}", pollH.handleUpdate)
				r.Delete("/{id}", pollH.handleDelete)
				r.Post("/{id}/copy", pollH.handleCopy)
			})

			// Upload routes (added in subsequent tasks)
			r.Route("/upload", func(r chi.Router) {})

			// Room routes (added in subsequent tasks)
			r.Route("/rooms", func(r chi.Router) {})
		})
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
