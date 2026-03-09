package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
	"presentarium/internal/ws"
)

// RouterDeps holds dependencies needed to build the router.
type RouterDeps struct {
	AuthService         service.AuthService
	PollService         service.PollService
	QuestionService     service.QuestionService
	RoomService         service.RoomService
	ParticipantService  service.ParticipantService
	ConductService      service.ConductService
	WSHandler           *ws.Handler
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

	// Wire WS join/leave hooks so the participant service manages DB records.
	if deps.WSHandler != nil && deps.ParticipantService != nil {
		deps.WSHandler.SetJoinLeaveHandlers(
			deps.ParticipantService.OnJoin,
			deps.ParticipantService.OnLeave,
		)
	}

	// WebSocket endpoint (no JWT middleware — auth is handled inside the WS handler).
	if deps.WSHandler != nil {
		r.Get("/ws/room/{code}", deps.WSHandler.HandleRoom)
	}

	// Auth rate limiter: 5 req/min per IP
	authLimiter := appmw.NewRateLimiter(5, time.Minute)

	// Wire conduct service as the WebSocket message handler.
	if deps.WSHandler != nil && deps.ConductService != nil {
		deps.WSHandler.SetMessageHandler(deps.ConductService.HandleMessage)
	}

	authH := newAuthHandler(deps.AuthService, deps.RefreshTokenTTLDays)
	pollH := newPollHandler(deps.PollService)
	questionH := newQuestionHandler(deps.QuestionService)
	roomH := newRoomHandler(deps.RoomService, deps.ConductService)
	participantH := newParticipantHandler(deps.ParticipantService)

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

				// Question routes nested under polls
				r.Route("/{pollId}/questions", func(r chi.Router) {
					r.Get("/", questionH.handleList)
					r.Post("/", questionH.handleCreate)
					r.Patch("/reorder", questionH.handleReorder)
					r.Put("/{id}", questionH.handleUpdate)
					r.Delete("/{id}", questionH.handleDelete)
				})
			})

			// Upload routes (added in subsequent tasks)
			r.Route("/upload", func(r chi.Router) {})

			// Room routes
			r.Route("/rooms", func(r chi.Router) {
				r.Post("/", roomH.handleCreate)
				r.Get("/{code}", roomH.handleGet)
				r.Patch("/{code}/state", roomH.handleChangeState)
				r.Get("/{code}/participants", participantH.handleList)
			})
		})
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
