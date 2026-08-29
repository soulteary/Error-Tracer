package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/Error-Tracer/internal/store"
)

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
