package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/caroline-amorim/test-case-pismo/internal/config"
	"github.com/caroline-amorim/test-case-pismo/internal/db/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("stopped", "reason", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DatabaseURL, cfg.DB)
	if err != nil {
		slog.Error("stopped", "reason", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	if err := postgres.Migrate(ctx, db); err != nil {
		slog.Error("stopped", "reason", err.Error())
		os.Exit(1)
	}
	slog.Info("migrated")
}
