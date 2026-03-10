package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"presentarium/internal/config"
	"presentarium/internal/handler"
	"presentarium/internal/repository"
	"presentarium/internal/service"
	"presentarium/internal/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := sqlx.Connect("postgres", cfg.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("connected to database")

	userRepo := repository.NewPostgresUserRepo(db)
	pollRepo := repository.NewPostgresPollRepo(db)
	questionRepo := repository.NewPostgresQuestionRepo(db)
	sessionRepo := repository.NewPostgresSessionRepo(db)
	participantRepo := repository.NewPostgresParticipantRepo(db)
	answerRepo := repository.NewPostgresAnswerRepo(db)

	authSvc := service.NewAuthService(
		userRepo,
		cfg.JWTSecret,
		cfg.JWTAccessTokenTTL,
		cfg.JWTRefreshTokenTTL,
	)
	pollSvc := service.NewPollService(pollRepo)
	questionSvc := service.NewQuestionService(questionRepo, pollRepo)

	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, cfg.JWTSecret)

	roomSvc := service.NewRoomService(sessionRepo, pollRepo, hub)
	participantSvc := service.NewParticipantService(participantRepo, sessionRepo, hub)
	conductSvc := service.NewConductService(questionRepo, sessionRepo, pollRepo, answerRepo, hub)

	router := handler.NewRouter(handler.RouterDeps{
		AuthService:         authSvc,
		PollService:         pollSvc,
		QuestionService:     questionSvc,
		RoomService:         roomSvc,
		ParticipantService:  participantSvc,
		ConductService:      conductSvc,
		WSHandler:           wsHandler,
		JWTSecret:           cfg.JWTSecret,
		RefreshTokenTTLDays: cfg.JWTRefreshTokenTTL,
		UploadsDir:          cfg.UploadsDir,
	})

	addr := fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort)
	slog.Info("server starting", "addr", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
