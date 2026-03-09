// cmd/migrate/main.go — standalone CLI tool for running database migrations.
//
// Usage:
//
//	go run ./cmd/migrate up          # apply all pending migrations
//	go run ./cmd/migrate down        # roll back all migrations
//	go run ./cmd/migrate down 1      # roll back N steps
//	go run ./cmd/migrate version     # print current version
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"presentarium/internal/config"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down|version> [steps]")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "migrations"
	}

	m, err := migrate.New("file://"+migrationsPath, cfg.DSN())
	if err != nil {
		slog.Error("migrate init failed", "error", err)
		os.Exit(1)
	}
	defer m.Close()

	cmd := args[0]
	switch cmd {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			slog.Error("migrate up failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrate up: done")

	case "down":
		steps := -1 // -1 means all steps
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "steps must be a positive integer")
				os.Exit(1)
			}
			steps = n
		}
		if steps == -1 {
			if err := m.Down(); err != nil && err != migrate.ErrNoChange {
				slog.Error("migrate down failed", "error", err)
				os.Exit(1)
			}
		} else {
			if err := m.Steps(-steps); err != nil && err != migrate.ErrNoChange {
				slog.Error("migrate steps failed", "error", err)
				os.Exit(1)
			}
		}
		slog.Info("migrate down: done")

	case "version":
		v, dirty, err := m.Version()
		if err != nil && err != migrate.ErrNilVersion {
			slog.Error("migrate version failed", "error", err)
			os.Exit(1)
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}
