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
	"presentarium/internal/storage"
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
	PresentationService service.PresentationService
	WSHandler           *ws.Handler
	Storage             storage.Storage
	JWTSecret           string
	RefreshTokenTTLDays int
	// UploadsDir is kept for backward-compat: old image URLs in the DB
	// reference /uploads/images/... and are served from local disk. New
	// uploads go to Storage.
	UploadsDir        string
	CORSAllowedOrigin string
	AppBaseURL        string
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
	r.Head("/health", healthHandler)

	// Public participant history endpoint (no JWT required).
	if deps.HistoryService != nil {
		sessionHPublic := newSessionHandler(deps.HistoryService)
		r.Get("/api/sessions/by-token", sessionHPublic.handleGetByParticipantToken)
	}

	// Serve legacy /uploads/* files written to local disk before the Storage
	// migration. New uploads go to object storage and are served from the
	// S3_PUBLIC_BASE_URL directly. Remove this block once all legacy image
	// URLs have been migrated.
	if deps.UploadsDir != "" {
		uploadsAbsDir := filepath.Clean(deps.UploadsDir)
		r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsAbsDir))))
	}

	// Wire WS join/leave hooks so the participant service manages DB records.
	// On join we also let the conduct service replay the live session state
	// (current question, timer, leaderboard, …) so a freshly-(re)connected
	// client resumes mid-session instead of landing on the waiting screen.
	if deps.WSHandler != nil && deps.ParticipantService != nil {
		participantOnJoin := deps.ParticipantService.OnJoin
		conductSvc := deps.ConductService
		onJoin := func(c *ws.Client, room *ws.Room) {
			participantOnJoin(c, room)
			if conductSvc != nil {
				conductSvc.ReplayStateForClient(c, room)
			}
		}
		deps.WSHandler.SetJoinLeaveHandlers(
			onJoin,
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

	authH := newAuthHandler(deps.AuthService, deps.RefreshTokenTTLDays, deps.AppBaseURL)
	pollH := newPollHandler(deps.PollService)
	questionH := newQuestionHandler(deps.QuestionService)
	roomH := newRoomHandler(deps.RoomService, deps.ConductService)
	participantH := newParticipantHandler(deps.ParticipantService)
	uploadH := newUploadHandler(deps.Storage)
	sessionH := newSessionHandler(deps.HistoryService)
	moderationH := newModerationHandler(deps.ModerationService)
	var presentationH *presentationHandler
	if deps.PresentationService != nil {
		presentationH = newPresentationHandler(deps.PresentationService)
	}

	r.Route("/api", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Use(appmw.RateLimit(authLimiter))
			r.Post("/register", authH.handleRegister)
			r.Post("/login", authH.handleLogin)
			r.Post("/refresh", authH.handleRefresh)
			r.Post("/logout", authH.handleLogout)
			r.Post("/forgot-password", authH.handleForgotPassword)
			r.Post("/reset-password", authH.handleResetPassword)
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

			// Presentation routes: .pptx upload + slide metadata.
			if presentationH != nil {
				r.Route("/presentations", func(r chi.Router) {
					r.Get("/", presentationH.handleList)
					r.Post("/", presentationH.handleCreate)
					r.Get("/{id}", presentationH.handleGet)
					r.Delete("/{id}", presentationH.handleDelete)
				})
			}

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
				r.Post("/{id}/export/pdf", sessionH.handleExportPDF)
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
