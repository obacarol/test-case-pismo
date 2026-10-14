package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/api"
	"github.com/caroline-amorim/test-case-pismo/internal/config"
	"github.com/caroline-amorim/test-case-pismo/internal/db/postgres"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
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

	accounts := account.NewService(postgres.NewAccountRepository(db))
	transactions := transaction.NewService(accounts, postgres.NewTransactionRepository(db), time.Now)
	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           api.NewHandler(accounts, transactions, db, cfg.API),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("stopped", "reason", "shutdown")
			os.Exit(1)
		}
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		slog.Error("stopped", "reason", "listen")
		os.Exit(1)
	}
}
