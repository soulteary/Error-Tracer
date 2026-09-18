package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/soulteary/Error-Tracer/internal/buildinfo"
	"github.com/soulteary/Error-Tracer/internal/config"
	"github.com/soulteary/Error-Tracer/internal/healthcheck"
	appserver "github.com/soulteary/Error-Tracer/internal/server"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) >= 2 {
		// A subcommand never falls through to the serve path: `break` would
		// leave the switch rather than run(), so a typo such as
		// `error-tracer version --json` would open the production database
		// read-write and bind the listener.
		switch os.Args[1] {
		case "healthcheck":
			if len(os.Args) != 2 {
				return usage("error-tracer healthcheck")
			}
			address := os.Getenv("ERROR_TRACER_ADDRESS")
			if err := healthcheck.Check(context.Background(), address); err != nil {
				slog.Error("health check failed", "error", err)
				return 1
			}
			return 0
		case "demo":
			if len(os.Args) != 2 {
				return usage("error-tracer demo")
			}
			return runDemo()
		case "db":
			return runDatabaseCommand(os.Args[2:])
		case "version":
			if len(os.Args) != 2 {
				return usage("error-tracer version")
			}
			fmt.Println(buildinfo.Summary())
			return 0
		}
	}

	cfg, err := config.FromEnvironment()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		return 1
	}
	// Opening runs the migrations and the event-history reconcile, either of
	// which can take a while on a large database. A background context made
	// the process ignore SIGTERM until they finished, so a rolling update had
	// to wait out the whole startup before the container would stop.
	startupCtx, stopStartup := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	issueStore, err := store.OpenSQLiteWithOptions(
		startupCtx,
		cfg.DatabasePath,
		store.SQLiteOptions{
			MaxOpenConnections: cfg.SQLiteMaxOpenConnections,
			MaxEventsPerIssue:  cfg.MaxEventsPerIssue,
		},
	)
	stopStartup()
	if err != nil {
		if startupCtx.Err() != nil {
			slog.Info("startup interrupted before the database was ready")
			return 0
		}
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
		SDKCrossOrigin:     cfg.SDKCrossOrigin,
	})

	return serve(app, cfg.Address, cfg.ShutdownTimeout, func(ctx context.Context) func() {
		return startRetention(
			ctx, issueStore, cfg.ProjectID, cfg.RetentionDays, cfg.MaxIssues,
		)
	}, false)
}

// usage reports an invalid subcommand invocation and returns the exit code
// shared with `error-tracer db`.
func usage(invocation string) int {
	slog.Error("usage: " + invocation)
	return 2
}

func runDatabaseCommand(arguments []string) int {
	operation := ""
	destination := ""
	switch {
	case len(arguments) == 1 && arguments[0] == "check":
		operation = "check"
	case len(arguments) == 2 && arguments[0] == "backup" && strings.TrimSpace(arguments[1]) != "":
		operation = "backup"
		destination = arguments[1]
	default:
		return usage("error-tracer db check | error-tracer db backup <destination>")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	databasePath := config.DatabasePathFromEnvironment()
	database, err := store.OpenSQLiteReadOnly(ctx, databasePath)
	if err != nil {
		slog.Error("open database for maintenance", "error", err)
		return 1
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("close maintenance database", "error", err)
		}
	}()

	if operation == "check" {
		if err := database.IntegrityCheck(ctx); err != nil {
			slog.Error("database integrity check failed", "error", err)
			return 1
		}
		slog.Info("database integrity check passed", "database", databasePath)
		return 0
	}
	if err := database.Backup(ctx, destination); err != nil {
		slog.Error("database backup failed", "error", err)
		return 1
	}
	slog.Info("database backup completed", "destination", strings.TrimSpace(destination))
	return 0
}

func runDemo() int {
	address := demoAddress(os.Getenv("ERROR_TRACER_ADDRESS"))
	slog.Info("demo workspace available", "url", demoURL(address))
	app := appserver.New(appserver.Options{DemoOnly: true})
	return serve(app, address, 10*time.Second, nil, true)
}

func demoURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "http://" + address + "/?demo=1"
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/?demo=1"
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

	info := buildinfo.Current()
	slog.Info(
		"starting Error-Tracer", "address", address, "demo_only", demoOnly,
		"version", info.Version, "commit", info.Commit,
	)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped unexpectedly", "error", err)
		stop()
		<-shutdownDone
		return 1
	}
	<-shutdownDone
	return 0
}

const (
	retentionSweepInterval = 24 * time.Hour
	// The cardinality cap runs far more often than the age sweep. Age is a
	// slow-moving property, but issue count is driven by what reporters send,
	// so a daily cap would let a full day of ingestion past the limit before
	// it took effect. The sweep is cheap when the project is under the limit:
	// the OFFSET skips every row and the DELETE matches nothing.
	issueLimitSweepInterval = 5 * time.Minute
)

type issuePruner interface {
	PruneIssues(context.Context, string, time.Time) (int64, error)
	EnforceIssueLimit(context.Context, string, int) (int64, error)
}

// startRetention runs age-based cleanup and, when maxIssues is positive, a
// cardinality cap. The two bound different things: a fingerprint includes the
// client-supplied message, so an issue count is driven by what reporters send
// rather than by how long data is kept, and age alone cannot bound it.
func startRetention(
	parent context.Context, pruner issuePruner, projectID string, days, maxIssues int,
) func() {
	if days <= 0 && maxIssues <= 0 {
		slog.Warn(
			"issue storage is unbounded: set ERROR_TRACER_RETENTION_DAYS, " +
				"ERROR_TRACER_MAX_ISSUES, or both",
		)
		return func() {}
	}
	if days <= 0 {
		slog.Warn(
			"age-based cleanup is disabled; only the issue cap bounds storage",
			"max_issues", maxIssues,
		)
	}

	ctx, cancel := context.WithCancel(parent)
	sweepAge := func() {
		if days <= 0 {
			return
		}
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
	sweepLimit := func() {
		if maxIssues <= 0 {
			return
		}
		evicted, err := enforceIssueLimit(ctx, pruner, projectID, maxIssues)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("enforce issue limit", "error", err)
			}
			return
		}
		if evicted > 0 {
			slog.Info("evicted issues over the limit", "evicted", evicted, "max_issues", maxIssues)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// The first sweep runs here rather than inline: it issues one DELETE
		// transaction per PruneBatchSize rows against SQLite's single writer,
		// so running it before ListenAndServe delayed the listener in
		// proportion to the backlog.
		sweepAge()
		sweepLimit()

		// A nil channel blocks forever in select, so a disabled bound simply
		// never fires.
		var ageTicks, limitTicks <-chan time.Time
		if days > 0 {
			ageTicker := time.NewTicker(retentionSweepInterval)
			defer ageTicker.Stop()
			ageTicks = ageTicker.C
		}
		if maxIssues > 0 {
			limitTicker := time.NewTicker(issueLimitSweepInterval)
			defer limitTicker.Stop()
			limitTicks = limitTicker.C
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ageTicks:
				sweepAge()
				// Age pruning frees headroom, so re-check the cap with it.
				sweepLimit()
			case <-limitTicks:
				sweepLimit()
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// enforceIssueLimit evicts oldest-first until the project is at or below
// limit, in the same bounded batches the age sweep uses.
func enforceIssueLimit(
	ctx context.Context, pruner issuePruner, projectID string, limit int,
) (int64, error) {
	var total int64
	for {
		evicted, err := pruner.EnforceIssueLimit(ctx, projectID, limit)
		total += evicted
		if err != nil {
			return total, err
		}
		if evicted < store.PruneBatchSize {
			return total, nil
		}
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
	var total int64
	for {
		deleted, err := pruner.PruneIssues(ctx, projectID, cutoff)
		total += deleted
		if err != nil {
			return total, err
		}
		if deleted < store.PruneBatchSize {
			return total, nil
		}
	}
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
