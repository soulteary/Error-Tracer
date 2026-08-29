package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestEventOriginPolicy(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string
		origin     string
		wantStatus int
		wantAllow  string
		wantVary   string
		wantStored int
	}{
		{
			name:       "trusted browser",
			allowed:    []string{"https://app.example.com"},
			origin:     "https://app.example.com",
			wantStatus: http.StatusAccepted,
			wantAllow:  "https://app.example.com",
			wantVary:   "Origin",
			wantStored: 1,
		},
		{
			name:       "case insensitive origin",
			allowed:    []string{"https://app.example.com"},
			origin:     "HTTPS://APP.EXAMPLE.COM",
			wantStatus: http.StatusAccepted,
			wantAllow:  "https://app.example.com",
			wantVary:   "Origin",
			wantStored: 1,
		},
		{
			name:       "untrusted browser",
			allowed:    []string{"https://app.example.com"},
			origin:     "https://attacker.example",
			wantStatus: http.StatusForbidden,
			wantStored: 0,
		},
		{
			name:       "browser disabled by default",
			origin:     "https://app.example.com",
			wantStatus: http.StatusForbidden,
			wantStored: 0,
		},
		{
			name:       "trusted non-browser client",
			wantStatus: http.StatusAccepted,
			wantStored: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			memory := store.NewMemory()
			app := New(Options{
				Store:          memory,
				ProjectID:      "project-a",
				IngestKey:      "0123456789abcdef",
				AllowedOrigins: test.allowed,
			})
			request := eventRequest(http.MethodPost)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()

			app.Handler().ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != test.wantAllow {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, test.wantAllow)
			}
			if got := response.Header().Get("Vary"); got != test.wantVary {
				t.Fatalf("Vary = %q, want %q", got, test.wantVary)
			}
			page, err := memory.ListIssues(context.Background(), "project-a", store.ListOptions{})
			if err != nil {
				t.Fatalf("list stored issues: %v", err)
			}
			if page.Total != test.wantStored {
				t.Fatalf("stored issues = %d, want %d", page.Total, test.wantStored)
			}
		})
	}
}

func TestEventOriginRejectsRepeatedHeader(t *testing.T) {
	app := originTestServer()
	request := eventRequest(http.MethodPost)
	request.Header.Add("Origin", "https://app.example.com")
	request.Header.Add("Origin", "https://app.example.com")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestEventPreflight(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		method     string
		headers    string
		wantStatus int
	}{
		{
			name:       "content type",
			origin:     "https://app.example.com",
			method:     http.MethodPost,
			headers:    "Content-Type",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "simple header set",
			origin:     "https://app.example.com",
			method:     http.MethodPost,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "missing origin",
			method:     http.MethodPost,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "untrusted origin",
			origin:     "https://attacker.example",
			method:     http.MethodPost,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "wrong method",
			origin:     "https://app.example.com",
			method:     http.MethodGet,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "credential header",
			origin:     "https://app.example.com",
			method:     http.MethodPost,
			headers:    "Content-Type, Authorization",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := originTestServer()
			request := httptest.NewRequest(http.MethodOptions, "/api/v1/events", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			request.Header.Set("Access-Control-Request-Method", test.method)
			if test.headers != "" {
				request.Header.Set("Access-Control-Request-Headers", test.headers)
			}
			response := httptest.NewRecorder()

			app.Handler().ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusNoContent {
				if got := response.Header().Get("Access-Control-Allow-Origin"); got != test.origin {
					t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, test.origin)
				}
				if got := response.Header().Get("Access-Control-Allow-Methods"); got != http.MethodPost {
					t.Fatalf("Access-Control-Allow-Methods = %q, want POST", got)
				}
				if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
					t.Fatalf("Access-Control-Allow-Headers = %q, want Content-Type", got)
				}
				if got := response.Header().Get("Access-Control-Max-Age"); got != "600" {
					t.Fatalf("Access-Control-Max-Age = %q, want 600", got)
				}
			} else {
				var result errorResponse
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if result.Error != "origin_not_allowed" {
					t.Fatalf("error = %q, want origin_not_allowed", result.Error)
				}
			}
		})
	}
}

func originTestServer() *Server {
	return New(Options{
		Store:          store.NewMemory(),
		ProjectID:      "project-a",
		IngestKey:      "0123456789abcdef",
		AllowedOrigins: []string{"https://app.example.com"},
	})
}

func eventRequest(method string) *http.Request {
	body := `{"project_key":"0123456789abcdef","event":{"kind":"error","message":"boom"}}`
	request := httptest.NewRequest(method, "/api/v1/events", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
