package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeHTTPServer struct {
	shutdownErr error
	closeErr    error
	closed      bool
}

type fakeIssuePruner struct {
	projectID string
	cutoff    time.Time
	deleted   int64
	err       error
}

func (p *fakeIssuePruner) PruneIssues(_ context.Context, projectID string, cutoff time.Time) (int64, error) {
	p.projectID = projectID
	p.cutoff = cutoff
	return p.deleted, p.err
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
	pruner := &fakeIssuePruner{deleted: 7}
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
