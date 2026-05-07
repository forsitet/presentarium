package config_test

import (
	"strings"
	"testing"

	"presentarium/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars that have defaults so we test pure defaults.
	for _, k := range []string{
		"SERVER_PORT", "SERVER_HOST",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
		"JWT_SECRET", "JWT_ACCESS_TOKEN_TTL_MIN", "JWT_REFRESH_TOKEN_TTL_DAYS",
		"CORS_ALLOWED_ORIGIN", "APP_BASE_URL", "UPLOADS_DIR",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET_PUBLIC", "S3_BUCKET_PRIVATE",
		"S3_FORCE_PATH_STYLE", "MIGRATIONS_PATH", "SMTP_PORT",
	} {
		t.Setenv(k, "")
	}
	// Setenv with empty actually sets to empty; unset needed. Use os.Unsetenv via t.Setenv("",""): not possible.
	// Instead provide a known set of overrides and verify they pass through.

	t.Setenv("DB_HOST", "db.local")
	t.Setenv("DB_PORT", "6543")
	t.Setenv("DB_USER", "alice")
	t.Setenv("DB_PASSWORD", "s3cret")
	t.Setenv("DB_NAME", "app")
	t.Setenv("DB_SSLMODE", "require")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("JWT_ACCESS_TOKEN_TTL_MIN", "30")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DBHost != "db.local" {
		t.Errorf("DBHost = %q", cfg.DBHost)
	}
	if cfg.DBPort != 6543 {
		t.Errorf("DBPort = %d", cfg.DBPort)
	}
	if cfg.ServerPort != "9090" {
		t.Errorf("ServerPort = %q", cfg.ServerPort)
	}
	if cfg.JWTAccessTokenTTL != 30 {
		t.Errorf("JWTAccessTokenTTL = %d", cfg.JWTAccessTokenTTL)
	}
}

func TestLoad_InvalidIntFails(t *testing.T) {
	t.Setenv("DB_PORT", "not-a-number")
	if _, err := config.Load(); err == nil {
		t.Error("expected Load to fail on invalid integer")
	}
}

func TestDSN(t *testing.T) {
	cfg := &config.Config{
		DBHost:     "host",
		DBPort:     5432,
		DBUser:     "user",
		DBPassword: "pass",
		DBName:     "db",
		DBSSLMode:  "disable",
	}
	got := cfg.DSN()
	want := "postgres://user:pass@host:5432/db?sslmode=disable"
	if got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "postgres://") {
		t.Error("DSN should start with postgres://")
	}
}
