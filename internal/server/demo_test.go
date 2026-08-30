package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestDemoMetadataIsDisabledByDefault(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var metadata publicMetadataResponse
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata.DemoMode {
		t.Fatal("demo_mode = true, want false")
	}
	if metadata.Version != "2.0.0-dev" {
		t.Fatalf("version = %q, want development version", metadata.Version)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestDemoRoutesAreUnavailableWhenDisabled(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/demo/issues", nil),
	)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestDemoModeServesIsolatedReadOnlyFixtures(t *testing.T) {
	liveStore := store.NewMemory()
	privateEvent := event.Event{
		Kind:       event.KindError,
		Message:    "private production failure",
		ReceivedAt: time.Now().UTC(),
	}
	if _, err := liveStore.Record(context.Background(), "project-a", privateEvent); err != nil {
		t.Fatalf("record private event: %v", err)
	}
	app := New(Options{
		Store:      liveStore,
		ProjectID:  "project-a",
		IngestKey:  "0123456789abcdef",
		AdminToken: testAdminToken,
		DemoMode:   true,
	})

	metadataResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		metadataResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil),
	)
	var metadata publicMetadataResponse
	if err := json.NewDecoder(metadataResponse.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if !metadata.DemoMode {
		t.Fatal("demo_mode = false, want true")
	}

	pageResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		pageResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/demo/issues?limit=10", nil),
	)
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", pageResponse.Code, http.StatusOK, pageResponse.Body.String())
	}
	var page store.IssuePage
	if err := json.NewDecoder(pageResponse.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 5 || len(page.Issues) != 5 {
		t.Fatalf("demo page has total %d and %d issues, want 5", page.Total, len(page.Issues))
	}
	var totalOccurrences uint64
	statuses := make(map[store.IssueStatus]bool)
	for _, issue := range page.Issues {
		if strings.Contains(issue.Message, "private production") {
			t.Fatal("demo response exposed an issue from the live store")
		}
		totalOccurrences += issue.Occurrences
		statuses[issue.Status] = true
	}
	if totalOccurrences != 106 {
		t.Fatalf("demo occurrences = %d, want 106", totalOccurrences)
	}
	for _, status := range []store.IssueStatus{
		store.IssueStatusOpen,
		store.IssueStatusResolved,
		store.IssueStatusIgnored,
	} {
		if !statuses[status] {
			t.Fatalf("demo page does not include status %q", status)
		}
	}

	detailResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		detailResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/demo/issues/"+page.Issues[0].Fingerprint, nil),
	)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d", detailResponse.Code, http.StatusOK)
	}

	patchResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		patchResponse,
		httptest.NewRequest(http.MethodPatch, "/api/v1/demo/issues/"+page.Issues[0].Fingerprint, strings.NewReader(`{"status":"ignored"}`)),
	)
	if patchResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("demo PATCH status = %d, want %d", patchResponse.Code, http.StatusMethodNotAllowed)
	}

	liveResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		liveResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil),
	)
	if liveResponse.Code != http.StatusUnauthorized {
		t.Fatalf("live API status = %d, want %d", liveResponse.Code, http.StatusUnauthorized)
	}
}

func TestDemoOnlyModeIsReadyWithoutPrivateServices(t *testing.T) {
	app := New(Options{DemoOnly: true})

	readyResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		readyResponse,
		httptest.NewRequest(http.MethodGet, "/readyz", nil),
	)
	if readyResponse.Code != http.StatusOK {
		t.Fatalf("readiness status = %d, want %d", readyResponse.Code, http.StatusOK)
	}

	metadataResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		metadataResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil),
	)
	var metadata publicMetadataResponse
	if err := json.NewDecoder(metadataResponse.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if !metadata.DemoMode || !metadata.DemoOnly {
		t.Fatalf("metadata = %+v, want demo_mode and demo_only", metadata)
	}

	demoResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		demoResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/demo/issues?limit=1", nil),
	)
	if demoResponse.Code != http.StatusOK {
		t.Fatalf("demo status = %d, want %d", demoResponse.Code, http.StatusOK)
	}

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil),
		httptest.NewRequest(http.MethodPatch, "/api/v1/issues/"+strings.Repeat("0", 64), strings.NewReader(`{}`)),
	} {
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusNotFound)
		}
	}
}

func TestDemoListValidatesFilters(t *testing.T) {
	app := New(Options{
		Store:      store.NewMemory(),
		ProjectID:  "project-a",
		IngestKey:  "0123456789abcdef",
		AdminToken: testAdminToken,
		DemoMode:   true,
	})

	for _, target := range []string{
		"/api/v1/demo/issues?limit=0",
		"/api/v1/demo/issues?status=closed",
	} {
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", target, response.Code, http.StatusBadRequest)
		}
	}
}
