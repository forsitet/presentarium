package handler

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	HistoryService      service.HistoryService
	ModerationService   service.ModerationService
	WSHandler           *ws.Handler
	JWTSecret           string
	RefreshTokenTTLDays int
	UploadsDir          string
	CORSAllowedOrigin   string
}

// NewRouter creates and configures the HTTP router.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()

	r.Use(appmw.CORS(deps.CORSAllowedOrigin))
	r.Use(appmw.Logger())
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	r.Get("/health", healthHandler)

	// Public participant history endpoint (no JWT required).
	if deps.HistoryService != nil {
		sessionHPublic := newSessionHandler(deps.HistoryService)
		r.Get("/api/sessions/by-token", sessionHPublic.handleGetByParticipantToken)
	}

	// Serve uploaded files (images) from the uploads directory.
	uploadsAbsDir := filepath.Clean(deps.UploadsDir)
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsAbsDir))))

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
	uploadH := newUploadHandler(deps.UploadsDir)
	sessionH := newSessionHandler(deps.HistoryService)
	moderationH := newModerationHandler(deps.ModerationService)

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

			// Upload routes
			r.Route("/upload", func(r chi.Router) {
				r.Post("/image", uploadH.handleImage)
			})

			// Room routes
			r.Route("/rooms", func(r chi.Router) {
				r.Post("/", roomH.handleCreate)
				r.Get("/{code}", roomH.handleGet)
				r.Patch("/{code}/state", roomH.handleChangeState)
				r.Get("/{code}/participants", participantH.handleList)
			})

			// Session history routes (read-only, organizer-owned)
			r.Route("/sessions", func(r chi.Router) {
				r.Get("/", sessionH.handleList)
				r.Get("/{id}", sessionH.handleGet)
				r.Get("/{id}/participants", sessionH.handleListParticipants)
				r.Get("/{id}/answers", sessionH.handleListAnswers)
				r.Get("/{id}/export/csv", sessionH.handleExportCSV)
				// Moderation: hide/show answers and brainstorm ideas
				r.Patch("/{sessionId}/answers/{answerId}", moderationH.handleHideAnswer)
				r.Patch("/{sessionId}/ideas/{ideaId}", moderationH.handleHideIdea)
			})
		})
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
