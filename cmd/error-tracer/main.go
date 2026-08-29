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
	"github.com/soulteary/Error-Tracer/internal/healthcheck"
	appserver "github.com/soulteary/Error-Tracer/internal/server"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		address := os.Getenv("ERROR_TRACER_ADDRESS")
		if err := healthcheck.Check(context.Background(), address); err != nil {
			slog.Error("health check failed", "error", err)
			return 1
		}
		return 0
	}

	cfg, err := config.FromEnvironment()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		return 1
	}
	issueStore, err := store.OpenSQLite(context.Background(), cfg.DatabasePath)
	if err != nil {
		slog.Error("open issue database", "error", err)
		return 1
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
		DemoMode:       cfg.DemoMode,
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

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		app.SetReady(false)
		if err := stopHTTPServer(httpServer, cfg.ShutdownTimeout); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("starting Error-Tracer", "address", cfg.Address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped unexpectedly", "error", err)
		return 1
	}
	<-shutdownDone
	return 0
}

type httpShutdowner interface {
	Shutdown(context.Context) error
	Close() error
}

func stopHTTPServer(server httpShutdowner, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		if closeErr := server.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return nil
}
