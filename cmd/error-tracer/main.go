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

	"github.com/soulteary/Error-Tracer/internal/config"
	appserver "github.com/soulteary/Error-Tracer/internal/server"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func main() {
	cfg, err := config.FromEnvironment()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	issueStore, err := store.OpenSQLite(context.Background(), cfg.DatabasePath)
	if err != nil {
		slog.Error("open issue database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := issueStore.Close(); err != nil {
			slog.Error("close issue database", "error", err)
		}
	}()

	app := appserver.New(appserver.Options{
		Store:          issueStore,
		ProjectID:      cfg.ProjectID,
		IngestKey:      cfg.IngestKey,
		AdminToken:     cfg.AdminToken,
		AllowedOrigins: cfg.AllowedOrigins,
		RatePerMinute:  cfg.RatePerMinute,
		RateBurst:      cfg.RateBurst,
	})

	httpServer := &http.Server{
		Addr:              cfg.Address,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		app.SetReady(false)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("starting Error-Tracer", "address", cfg.Address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
