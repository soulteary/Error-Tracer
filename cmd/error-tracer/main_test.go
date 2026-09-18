package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appserver "github.com/soulteary/Error-Tracer/internal/server"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestServeWaitsForShutdownAfterListenFailure(t *testing.T) {
	app := appserver.New(appserver.Options{DemoOnly: true})
	stoppedAfterCancel := false
	code := serve(
		app,
		"127.0.0.1:-1",
		time.Second,
		func(ctx context.Context) func() {
			return func() {
				stoppedAfterCancel = ctx.Err() != nil
			}
		},
		true,
	)
	if code != 1 {
		t.Fatalf("serve() = %d, want 1", code)
	}
	if !stoppedAfterCancel {
		t.Fatal("background work stopped before the shutdown context was canceled")
	}

	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

type fakeHTTPServer struct {
	shutdownErr error
	closeErr    error
	closed      bool
}

type fakeIssuePruner struct {
	projectID string
	cutoff    time.Time
	remaining int64
	calls     int
	err       error

	limit        int
	overLimit    int64
	limitCalls   int
	limitProject string
	limitErr     error
}

func (p *fakeIssuePruner) PruneIssues(_ context.Context, projectID string, cutoff time.Time) (int64, error) {
	p.projectID = projectID
	p.cutoff = cutoff
	p.calls++
	deleted := min(p.remaining, int64(store.PruneBatchSize))
	p.remaining -= deleted
	return deleted, p.err
}

func (p *fakeIssuePruner) EnforceIssueLimit(
	_ context.Context, projectID string, limit int,
) (int64, error) {
	p.limitProject = projectID
	p.limit = limit
	p.limitCalls++
	evicted := min(p.overLimit, int64(store.PruneBatchSize))
	p.overLimit -= evicted
	return evicted, p.limitErr
}

func (s *fakeHTTPServer) Shutdown(context.Context) error {
	return s.shutdownErr
}

func (s *fakeHTTPServer) Close() error {
	s.closed = true
	return s.closeErr
}

func TestStopHTTPServerGraceful(t *testing.T) {
	server := &fakeHTTPServer{}
	if err := stopHTTPServer(server, time.Second); err != nil {
		t.Fatalf("stopHTTPServer() error = %v", err)
	}
	if server.closed {
		t.Fatal("Close() called after a successful graceful shutdown")
	}
}

func TestStopHTTPServerForcesClose(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	server := &fakeHTTPServer{shutdownErr: shutdownErr}
	if err := stopHTTPServer(server, time.Second); !errors.Is(err, shutdownErr) {
		t.Fatalf("stopHTTPServer() error = %v, want %v", err, shutdownErr)
	}
	if !server.closed {
		t.Fatal("Close() was not called after graceful shutdown failed")
	}
}

func TestStopHTTPServerReportsCloseFailure(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	closeErr := errors.New("close failed")
	server := &fakeHTTPServer{shutdownErr: shutdownErr, closeErr: closeErr}
	err := stopHTTPServer(server, time.Second)
	if !errors.Is(err, shutdownErr) || !errors.Is(err, closeErr) {
		t.Fatalf("stopHTTPServer() error = %v, want both shutdown and close errors", err)
	}
}

func TestPruneExpiredIssuesUsesConfiguredWindow(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.FixedZone("test", 9*60*60))
	pruner := &fakeIssuePruner{remaining: 7}
	deleted, err := pruneExpiredIssues(context.Background(), pruner, "project-a", 30, now)
	if err != nil {
		t.Fatalf("pruneExpiredIssues() error = %v", err)
	}
	if deleted != 7 || pruner.projectID != "project-a" {
		t.Fatalf("deleted = %d, project = %q", deleted, pruner.projectID)
	}
	wantCutoff := now.UTC().Add(-30 * 24 * time.Hour)
	if !pruner.cutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %s, want %s", pruner.cutoff, wantCutoff)
	}
}

func TestPruneExpiredIssuesRepeatsBoundedDeletes(t *testing.T) {
	pruner := &fakeIssuePruner{remaining: int64(store.PruneBatchSize*2 + 3)}
	deleted, err := pruneExpiredIssues(
		context.Background(), pruner, "project-a", 30, time.Now(),
	)
	if err != nil {
		t.Fatalf("pruneExpiredIssues() error = %v", err)
	}
	if deleted != int64(store.PruneBatchSize*2+3) || pruner.calls != 3 {
		t.Fatalf("deleted = %d, calls = %d, want %d in 3 calls", deleted, pruner.calls, store.PruneBatchSize*2+3)
	}
}

func TestDatabaseMaintenanceCommands(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	backup := filepath.Join(directory, "backup.db")
	database, err := store.OpenSQLite(context.Background(), source)
	if err != nil {
		t.Fatalf("create source database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close source database: %v", err)
	}
	t.Setenv("ERROR_TRACER_DATABASE_PATH", source)
	t.Setenv("ERROR_TRACER_INGEST_KEY", "")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "")

	if code := runDatabaseCommand([]string{"check"}); code != 0 {
		t.Fatalf("db check exit code = %d, want 0", code)
	}
	if code := runDatabaseCommand([]string{"backup", backup}); code != 0 {
		t.Fatalf("db backup exit code = %d, want 0", code)
	}
	if code := runDatabaseCommand([]string{"backup", backup}); code != 1 {
		t.Fatalf("existing backup exit code = %d, want 1", code)
	}
	if code := runDatabaseCommand([]string{"unknown"}); code != 2 {
		t.Fatalf("invalid command exit code = %d, want 2", code)
	}
}

func TestDemoAddressDefaultsToLoopback(t *testing.T) {
	if got := demoAddress(""); got != "127.0.0.1:8080" {
		t.Fatalf("demoAddress() = %q, want loopback default", got)
	}
	if got := demoAddress("  :9090  "); got != ":9090" {
		t.Fatalf("demoAddress() = %q, want trimmed override", got)
	}
}

func TestDemoURLUsesABrowserReachableHost(t *testing.T) {
	for _, test := range []struct {
		address string
		want    string
	}{
		{":8080", "http://127.0.0.1:8080/?demo=1"},
		{"0.0.0.0:9090", "http://127.0.0.1:9090/?demo=1"},
		{"[::]:8080", "http://[::1]:8080/?demo=1"},
		{"localhost:8080", "http://localhost:8080/?demo=1"},
	} {
		if got := demoURL(test.address); got != test.want {
			t.Errorf("demoURL(%q) = %q, want %q", test.address, got, test.want)
		}
	}
}

func TestRunRejectsMalformedSubcommands(t *testing.T) {
	// A guard that used `break` left the switch rather than run(), so these
	// invocations fell through to the serve path and opened the configured
	// database read-write instead of reporting a usage error.
	database := filepath.Join(t.TempDir(), "error-tracer.db")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", database)
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdef0123456789")
	t.Setenv("ERROR_TRACER_ADDRESS", "127.0.0.1:0")

	for _, arguments := range [][]string{
		{"version", "--json"},
		{"healthcheck", "-v"},
		{"demo", "extra"},
		{"db"},
		{"db", "check", "extra"},
		{"db", "backup"},
		{"db", "backup", "  "},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			restore := os.Args
			os.Args = append([]string{"error-tracer"}, arguments...)
			t.Cleanup(func() { os.Args = restore })

			if code := run(); code != 2 {
				t.Fatalf("run(%q) = %d, want 2", arguments, code)
			}
			if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stat(%q) error = %v, want the database to stay untouched", database, err)
			}
		})
	}
}

func TestRunReportsVersion(t *testing.T) {
	restore := os.Args
	os.Args = []string{"error-tracer", "version"}
	t.Cleanup(func() { os.Args = restore })

	if code := run(); code != 0 {
		t.Fatalf("run(version) = %d, want 0", code)
	}
}

type blockingPruner struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (p *blockingPruner) EnforceIssueLimit(
	context.Context, string, int,
) (int64, error) {
	return 0, nil
}

func (p *blockingPruner) PruneIssues(
	ctx context.Context, _ string, _ time.Time,
) (int64, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	return 0, nil
}

func TestStartRetentionDoesNotBlockStartup(t *testing.T) {
	// The first sweep used to run inline, before ListenAndServe, so startup
	// was delayed in proportion to the expired-issue backlog.
	pruner := &blockingPruner{
		release: make(chan struct{}),
		started: make(chan struct{}),
	}
	returned := make(chan func(), 1)
	go func() {
		returned <- startRetention(context.Background(), pruner, "project-a", 30, 0)
	}()

	var stop func()
	select {
	case stop = <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("startRetention did not return while the first sweep was in flight")
	}

	select {
	case <-pruner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first sweep never ran")
	}

	close(pruner.release)
	stop()
}

func TestEnforceIssueLimitRepeatsBoundedEvictions(t *testing.T) {
	pruner := &fakeIssuePruner{overLimit: int64(store.PruneBatchSize*2 + 3)}

	evicted, err := enforceIssueLimit(context.Background(), pruner, "project-a", 1000)
	if err != nil {
		t.Fatalf("enforceIssueLimit() error = %v", err)
	}
	if evicted != int64(store.PruneBatchSize*2+3) || pruner.limitCalls != 3 {
		t.Fatalf("evicted = %d in %d calls, want %d in 3 calls",
			evicted, pruner.limitCalls, store.PruneBatchSize*2+3)
	}
	if pruner.limitProject != "project-a" || pruner.limit != 1000 {
		t.Fatalf("project = %q, limit = %d, want project-a and 1000",
			pruner.limitProject, pruner.limit)
	}
}

func TestStartRetentionRunsBothBounds(t *testing.T) {
	// Age and cardinality bound different things, so configuring either one
	// has to start the sweeper.
	tests := []struct {
		name      string
		days      int
		maxIssues int
		wantAge   bool
		wantLimit bool
	}{
		{name: "both", days: 30, maxIssues: 1000, wantAge: true, wantLimit: true},
		{name: "age only", days: 30, wantAge: true},
		{name: "limit only", maxIssues: 1000, wantLimit: true},
		{name: "neither"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pruner := &fakeIssuePruner{}
			stop := startRetention(
				context.Background(), pruner, "project-a", test.days, test.maxIssues,
			)
			stop()

			if gotAge := pruner.calls > 0; gotAge != test.wantAge {
				t.Fatalf("age sweep ran = %v, want %v", gotAge, test.wantAge)
			}
			if gotLimit := pruner.limitCalls > 0; gotLimit != test.wantLimit {
				t.Fatalf("limit sweep ran = %v, want %v", gotLimit, test.wantLimit)
			}
		})
	}
}
