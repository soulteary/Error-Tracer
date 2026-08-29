package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
