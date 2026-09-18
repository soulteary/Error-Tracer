package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestBrowserSDKAsset(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/assets/error-tracer.js", nil)
	response := httptest.NewRecorder()

	newTestServer().Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "ErrorTracer") {
		t.Fatal("browser SDK response does not contain the public API")
	}
	if got := response.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("ETag"); got == "" {
		t.Fatal("ETag is empty")
	}
}

func TestBrowserSDKAssetSupportsConditionalRequest(t *testing.T) {
	first := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(
		first,
		httptest.NewRequest(http.MethodGet, "/assets/error-tracer.js", nil),
	)

	request := httptest.NewRequest(http.MethodGet, "/assets/error-tracer.js", nil)
	request.Header.Set("If-None-Match", first.Header().Get("ETag"))
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotModified)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("conditional response body length = %d, want 0", response.Body.Len())
	}
}

func TestBrowserSDKAssetRestrictsMethod(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/assets/error-tracer.js", nil)
	response := httptest.NewRecorder()

	newTestServer().Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestBrowserSDKCrossOriginIsOptIn(t *testing.T) {
	// Subresource Integrity needs crossorigin="anonymous", which makes the
	// browser send Origin and refuse the script without a matching
	// Access-Control-Allow-Origin.
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/assets/error-tracer.js", nil)
		r.Header.Set("Origin", "https://app.example.com")
		return r
	}

	disabled := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(disabled, request())
	if got := disabled.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("default Access-Control-Allow-Origin = %q, want it unset", got)
	}

	app := New(Options{
		Store:          store.NewMemory(),
		ProjectID:      "project-a",
		IngestKey:      "0123456789abcdef",
		AdminToken:     "0123456789abcdefghijklmn",
		SDKCrossOrigin: true,
	})
	enabled := httptest.NewRecorder()
	app.Handler().ServeHTTP(enabled, request())
	if enabled.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", enabled.Code, http.StatusOK)
	}
	if got := enabled.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}

	// A conditional request has to carry it too, or the revalidated response
	// is unusable from the embedding page.
	conditional := request()
	conditional.Header.Set("If-None-Match", browserSDKETag)
	notModified := httptest.NewRecorder()
	app.Handler().ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", notModified.Code, http.StatusNotModified)
	}
	if got := notModified.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("304 Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}
