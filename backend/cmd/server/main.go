package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"presentarium/internal/config"
	"presentarium/internal/handler"
	"presentarium/internal/repository"
	"presentarium/internal/service"
	"presentarium/internal/ws"
	"presentarium/pkg/badwords"
	"presentarium/pkg/email"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Load badwords dictionary and start hot-reload watcher.
	const badwordsPath = "pkg/badwords/badwords.json"
	if err := badwords.LoadFromFile(badwordsPath); err != nil {
		slog.Warn("badwords dictionary not loaded", "error", err)
	} else {
		slog.Info("badwords dictionary loaded")
		badwords.WatchFile(badwordsPath, 10*time.Second)
	}

	db, err := sqlx.Connect("postgres", cfg.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("connected to database")

	// Run database migrations automatically on startup.
	if migrateErr := runMigrations(db, cfg.MigrationsPath); migrateErr != nil {
		slog.Error("migrations failed", "error", migrateErr)
		os.Exit(1)
	}

	userRepo := repository.NewPostgresUserRepo(db)
	pollRepo := repository.NewPostgresPollRepo(db)
	questionRepo := repository.NewPostgresQuestionRepo(db)
	sessionRepo := repository.NewPostgresSessionRepo(db)
	participantRepo := repository.NewPostgresParticipantRepo(db)
	answerRepo := repository.NewPostgresAnswerRepo(db)
	brainstormRepo := repository.NewPostgresBrainstormRepo(db)

	emailSender := email.NewSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFrom)
	if emailSender == nil {
		slog.Warn("SMTP not configured — password reset emails will not be sent")
	}

	authSvc := service.NewAuthService(
		userRepo,
		cfg.JWTSecret,
		cfg.JWTAccessTokenTTL,
		cfg.JWTRefreshTokenTTL,
		emailSender,
	)
	pollSvc := service.NewPollService(pollRepo)
	questionSvc := service.NewQuestionService(questionRepo, pollRepo)

	hub := ws.NewHub()
	wsHandler := ws.NewHandler(hub, cfg.JWTSecret)

	roomSvc := service.NewRoomService(sessionRepo, pollRepo, questionRepo, hub)
	participantSvc := service.NewParticipantService(participantRepo, sessionRepo, hub)
	conductSvc := service.NewConductService(questionRepo, sessionRepo, pollRepo, answerRepo, brainstormRepo, hub)
	historySvc := service.NewHistoryService(sessionRepo, answerRepo, participantRepo, questionRepo)
	moderationSvc := service.NewModerationService(sessionRepo, answerRepo, brainstormRepo, hub)

	router := handler.NewRouter(handler.RouterDeps{
		AuthService:         authSvc,
		PollService:         pollSvc,
		QuestionService:     questionSvc,
		RoomService:         roomSvc,
		ParticipantService:  participantSvc,
		ConductService:      conductSvc,
		HistoryService:      historySvc,
		ModerationService:   moderationSvc,
		WSHandler:           wsHandler,
		JWTSecret:           cfg.JWTSecret,
		RefreshTokenTTLDays: cfg.JWTRefreshTokenTTL,
		UploadsDir:          cfg.UploadsDir,
		CORSAllowedOrigin:   cfg.CORSAllowedOrigin,
		AppBaseURL:          cfg.AppBaseURL,
	})

	addr := fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort)
	slog.Info("server starting", "addr", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func runMigrations(db *sqlx.DB, migrationsPath string) error {
	driver, err := migratepostgres.WithInstance(db.DB, &migratepostgres.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("migrations applied")
	return nil
}
