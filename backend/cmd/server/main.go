package main

import (
	"context"
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
	"presentarium/internal/storage"
	"presentarium/internal/ws"
	"presentarium/pkg/badwords"
	"presentarium/pkg/email"
	"presentarium/pkg/pptx"
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

	// Object storage (MinIO/S3-compatible). Buckets are normally provisioned
	// by the minio-init compose service; EnsureBuckets is a belt-and-braces
	// idempotent check that also makes this work when the backend is started
	// outside compose (e.g. `go run ./cmd/server`).
	store, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:       cfg.S3Endpoint,
		Region:         cfg.S3Region,
		AccessKeyID:    cfg.S3AccessKeyID,
		SecretKey:      cfg.S3SecretKey,
		BucketPublic:   cfg.S3BucketPublic,
		BucketPrivate:  cfg.S3BucketPrivate,
		PublicBaseURL:  cfg.S3PublicBaseURL,
		ForcePathStyle: cfg.S3ForcePathStyle,
	})
	if err != nil {
		slog.Error("failed to init object storage", "error", err)
		os.Exit(1)
	}
	ensureCtx, cancelEnsure := context.WithTimeout(context.Background(), 10*time.Second)
	if err := store.EnsureBuckets(ensureCtx); err != nil {
		// Non-fatal: storage may still be reachable for reads. Log loudly so
		// the operator can investigate auth / network issues.
		slog.Warn("EnsureBuckets failed — uploads may not work", "error", err)
	}
	cancelEnsure()
	slog.Info("object storage ready", "endpoint", cfg.S3Endpoint, "public_bucket", cfg.S3BucketPublic)

	userRepo := repository.NewPostgresUserRepo(db)
	pollRepo := repository.NewPostgresPollRepo(db)
	questionRepo := repository.NewPostgresQuestionRepo(db)
	sessionRepo := repository.NewPostgresSessionRepo(db)
	participantRepo := repository.NewPostgresParticipantRepo(db)
	answerRepo := repository.NewPostgresAnswerRepo(db)
	brainstormRepo := repository.NewPostgresBrainstormRepo(db)
	presentationRepo := repository.NewPostgresPresentationRepo(db)

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
	historySvc := service.NewHistoryService(sessionRepo, answerRepo, participantRepo, questionRepo)
	moderationSvc := service.NewModerationService(sessionRepo, answerRepo, brainstormRepo, hub)
	presentationSvc := service.NewPresentationService(presentationRepo, store, pptx.NewCLIConverter())
	// conductSvc depends on presentationSvc for handling open_presentation /
	// change_slide / close_presentation WS messages, so build it after.
	conductSvc := service.NewConductService(questionRepo, sessionRepo, pollRepo, answerRepo, brainstormRepo, presentationSvc, hub)

	router := handler.NewRouter(handler.RouterDeps{
		AuthService:         authSvc,
		PollService:         pollSvc,
		QuestionService:     questionSvc,
		RoomService:         roomSvc,
		ParticipantService:  participantSvc,
		ConductService:      conductSvc,
		HistoryService:      historySvc,
		ModerationService:   moderationSvc,
		PresentationService: presentationSvc,
		WSHandler:           wsHandler,
		Storage:             store,
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
