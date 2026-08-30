package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/Error-Tracer/internal/store"
)

type checkedStore struct {
	store.Store
	err error
}

func (s *checkedStore) Ready(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("readiness context has no deadline")
	}
	return s.err
}

func TestHealth(t *testing.T) {
	app := newTestServer()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
	if got := response.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q, want health JSON", got)
	}
}

func TestReadinessTracksState(t *testing.T) {
	app := newTestServer()

	assertStatus := func(want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("status = %d, want %d", response.Code, want)
		}
	}

	assertStatus(http.StatusOK)
	app.SetReady(false)
	assertStatus(http.StatusServiceUnavailable)
}

func TestReadinessChecksAndRecoversTheIssueStore(t *testing.T) {
	backend := &checkedStore{
		Store: store.NewMemory(),
		err:   errors.New("database unavailable"),
	}
	app := New(Options{
		Store: backend, ProjectID: "project-a",
		IngestKey: "0123456789abcdef", AdminToken: "0123456789abcdefghijklmn",
		MetricsEnabled: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || app.storeReady.Load() {
		t.Fatalf("failed store readiness status = %d, storeReady = %t", response.Code, app.storeReady.Load())
	}
	metrics := httptest.NewRecorder()
	app.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if body := metrics.Body.String(); !strings.Contains(body, "error_tracer_ready 0") ||
		!strings.Contains(body, "error_tracer_store_ready 0") {
		t.Fatalf("failed readiness metrics are missing:\n%s", body)
	}

	backend.err = nil
	response = httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK || !app.storeReady.Load() {
		t.Fatalf("recovered store readiness status = %d, storeReady = %t", response.Code, app.storeReady.Load())
	}
	metrics = httptest.NewRecorder()
	app.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if body := metrics.Body.String(); !strings.Contains(body, "error_tracer_ready 1") ||
		!strings.Contains(body, "error_tracer_store_ready 1") {
		t.Fatalf("recovered readiness metrics are missing:\n%s", body)
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	app := newTestServer()
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func newTestServer() *Server {
	return New(Options{
		Store:      store.NewMemory(),
		ProjectID:  "project-a",
		IngestKey:  "0123456789abcdef",
		AdminToken: "0123456789abcdefghijklmn",
	})
}
