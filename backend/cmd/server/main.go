package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"presentarium/internal/config"
	"presentarium/internal/handler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	router := handler.NewRouter()

	addr := fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort)
	slog.Info("server starting", "addr", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
