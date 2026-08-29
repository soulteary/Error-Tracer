package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestMetricsAreDisabledByDefault(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestMetricsExposeBoundedPrometheusSeries(t *testing.T) {
	app := New(Options{
		Store:          store.NewMemory(),
		ProjectID:      "project-a",
		IngestKey:      "0123456789abcdef",
		AdminToken:     testAdminToken,
		MetricsEnabled: true,
	})

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	app.Handler().ServeHTTP(httptest.NewRecorder(), request)
	request = httptest.NewRequest(http.MethodDelete, "/not-found/attacker-controlled", nil)
	app.Handler().ServeHTTP(httptest.NewRecorder(), request)
	request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/events",
		strings.NewReader(`{"project_key":"0123456789abcdef","event":{"kind":"error","message":"boom"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	ingestResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(ingestResponse, request)
	if ingestResponse.Code != http.StatusAccepted {
		t.Fatalf("ingest status = %d, want %d; body = %s", ingestResponse.Code, http.StatusAccepted, ingestResponse.Body.String())
	}

	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != prometheusContentType {
		t.Fatalf("Content-Type = %q, want %q", got, prometheusContentType)
	}
	body := response.Body.String()
	for _, sample := range []string{
		`error_tracer_http_requests_total{method="GET",route="/healthz",status="200"} 1`,
		`error_tracer_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`,
		`error_tracer_http_requests_total{method="POST",route="/api/v1/events",status="202"} 1`,
		"error_tracer_http_request_duration_seconds_bucket",
		"error_tracer_http_in_flight_requests 0",
		"error_tracer_ingested_events_total 1",
		"error_tracer_ready 1",
		"error_tracer_demo_enabled 0",
	} {
		if !strings.Contains(body, sample) {
			t.Errorf("metrics body does not contain %q\n%s", sample, body)
		}
	}
	if strings.Contains(body, "attacker-controlled") || strings.Contains(body, `route="/metrics"`) {
		t.Fatalf("metrics contain an unbounded or self-scrape route:\n%s", body)
	}
}
