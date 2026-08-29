package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "healthcheck":
			address := os.Getenv("ERROR_TRACER_ADDRESS")
			if err := healthcheck.Check(context.Background(), address); err != nil {
				slog.Error("health check failed", "error", err)
				return 1
			}
			return 0
		case "demo":
			return runDemo()
		}
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
		Store:              issueStore,
		ProjectID:          cfg.ProjectID,
		IngestKey:          cfg.IngestKey,
		AdminToken:         cfg.AdminToken,
		PreviousAdminToken: cfg.PreviousAdminToken,
		AllowedOrigins:     cfg.AllowedOrigins,
		RatePerMinute:      cfg.RatePerMinute,
		RateBurst:          cfg.RateBurst,
		DemoMode:           cfg.DemoMode,
		MetricsEnabled:     cfg.MetricsEnabled,
	})

	return serve(app, cfg.Address, cfg.ShutdownTimeout, func(ctx context.Context) func() {
		return startRetention(ctx, issueStore, cfg.ProjectID, cfg.RetentionDays)
	}, false)
}

func runDemo() int {
	address := demoAddress(os.Getenv("ERROR_TRACER_ADDRESS"))
	app := appserver.New(appserver.Options{DemoOnly: true})
	return serve(app, address, 10*time.Second, nil, true)
}

func demoAddress(value string) string {
	if address := strings.TrimSpace(value); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}

type backgroundStarter func(context.Context) func()

func serve(
	app *appserver.Server,
	address string,
	shutdownTimeout time.Duration,
	startBackground backgroundStarter,
	demoOnly bool,
) int {
	httpServer := &http.Server{
		Addr:              address,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopBackground := func() {}
	if startBackground != nil {
		stopBackground = startBackground(ctx)
	}
	defer stopBackground()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		app.SetReady(false)
		if err := stopHTTPServer(httpServer, shutdownTimeout); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("starting Error-Tracer", "address", address, "demo_only", demoOnly)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped unexpectedly", "error", err)
		return 1
	}
	<-shutdownDone
	return 0
}

const retentionSweepInterval = 24 * time.Hour

type issuePruner interface {
	PruneIssues(context.Context, string, time.Time) (int64, error)
}

func startRetention(parent context.Context, pruner issuePruner, projectID string, days int) func() {
	if days <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(parent)
	sweep := func() {
		deleted, err := pruneExpiredIssues(ctx, pruner, projectID, days, time.Now().UTC())
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("prune expired issues", "error", err)
			}
			return
		}
		if deleted > 0 {
			slog.Info("pruned expired issues", "deleted", deleted, "retention_days", days)
		}
	}
	sweep()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(retentionSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep()
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func pruneExpiredIssues(
	ctx context.Context,
	pruner issuePruner,
	projectID string,
	days int,
	now time.Time,
) (int64, error) {
	cutoff := now.UTC().Add(-time.Duration(days) * 24 * time.Hour)
	return pruner.PruneIssues(ctx, projectID, cutoff)
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
