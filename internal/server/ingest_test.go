package server

import (
	"bytes"
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

func TestIngestEvent(t *testing.T) {
	memory := store.NewMemory()
	app := New(Options{
		Store:     memory,
		ProjectID: "project-a",
		IngestKey: "0123456789abcdef",
	})
	receivedAt := time.Date(2026, time.August, 29, 2, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return receivedAt }
	app.newID = func() (string, error) { return "evt_test", nil }

	body := `{
		"project_key":"0123456789abcdef",
		"event":{
			"id":"client-controlled",
			"kind":"error",
			"message":" boom ",
			"source_url":"https://example.com/app.js?v=1",
			"line":10,
			"received_at":"2020-01-01T00:00:00Z",
			"user_agent":"client-controlled"
		}
	}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "test-agent")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	var result ingestResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID != "evt_test" || result.Fingerprint == "" {
		t.Fatalf("response = %#v, want server ID and fingerprint", result)
	}

	page, err := memory.ListIssues(context.Background(), "project-a", store.ListOptions{})
	if err != nil {
		t.Fatalf("list stored issues: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("stored issues = %d, want 1", page.Total)
	}
	stored := page.Issues[0].LastEvent
	if stored.ID != "evt_test" || !stored.ReceivedAt.Equal(receivedAt) || stored.UserAgent != "test-agent" {
		t.Fatalf("server-controlled fields = %#v, want overwritten values", stored)
	}
	if stored.Message != "boom" || stored.SourceURL != "https://example.com/app.js" {
		t.Fatalf("normalized event = %#v", stored)
	}
}

func TestIngestEventAcceptsBeaconTextPlain(t *testing.T) {
	app := newTestServer()
	body := `{"project_key":"0123456789abcdef","event":{"kind":"error","message":"boom"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	request.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusAccepted, response.Body.String())
	}
}

func TestIngestEventRejectsInvalidRequests(t *testing.T) {
	valid := `{"project_key":"0123456789abcdef","event":{"kind":"error","message":"boom"}}`
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
		wantError   string
	}{
		{name: "media type", body: valid, contentType: "application/xml", wantStatus: http.StatusUnsupportedMediaType, wantError: "unsupported_media_type"},
		{name: "invalid JSON", body: `{`, contentType: "application/json", wantStatus: http.StatusBadRequest, wantError: "invalid_json"},
		{name: "unknown field", body: `{"unknown":true}`, contentType: "application/json", wantStatus: http.StatusBadRequest, wantError: "invalid_json"},
		{name: "multiple values", body: valid + `{}`, contentType: "application/json", wantStatus: http.StatusBadRequest, wantError: "invalid_json"},
		{name: "project key", body: `{"project_key":"wrong","event":{"kind":"error","message":"boom"}}`, contentType: "application/json", wantStatus: http.StatusUnauthorized, wantError: "invalid_project_key"},
		{name: "event", body: `{"project_key":"0123456789abcdef","event":{"kind":"error"}}`, contentType: "application/json", wantStatus: http.StatusUnprocessableEntity, wantError: "invalid_event"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestServer()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()

			app.Handler().ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var result errorResponse
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if result.Error != test.wantError {
				t.Fatalf("error = %q, want %q", result.Error, test.wantError)
			}
		})
	}
}

func TestIngestEventRejectsOversizedBody(t *testing.T) {
	app := newTestServer()
	body := append([]byte(`{"project_key":"0123456789abcdef","event":{"kind":"error","message":"`), bytes.Repeat([]byte("x"), maxEventBodySize+1)...)
	body = append(body, []byte(`"}}`)...)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}

func TestIngestEventRejectsUnconfiguredServer(t *testing.T) {
	app := New(Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestIngestEventReturnsValidationField(t *testing.T) {
	app := newTestServer()
	body := `{"project_key":"0123456789abcdef","event":{"kind":"error"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	var result errorResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Field != "message" {
		t.Fatalf("field = %q, want %q", result.Field, "message")
	}
}

func TestIngestMethodIsRestricted(t *testing.T) {
	app := newTestServer()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestRandomEventID(t *testing.T) {
	id, err := randomEventID()
	if err != nil {
		t.Fatalf("randomEventID() error = %v", err)
	}
	if !strings.HasPrefix(id, "evt_") || len(id) != len("evt_")+32 {
		t.Fatalf("id = %q, want evt_ followed by 32 hex characters", id)
	}
}

func TestIngestNormalizesBeforeValidation(t *testing.T) {
	captured := event.Event{Kind: event.KindError, Message: " boom "}
	captured.Normalize()
	if err := captured.Validate(); err != nil {
		t.Fatalf("normalized event validation: %v", err)
	}
}

func TestConstantTimeEqualSupportsVariableLengthCredentials(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "equal", left: "0123456789abcdef", right: "0123456789abcdef", want: true},
		{name: "same length mismatch", left: "0123456789abcdef", right: "0123456789abcdeg"},
		{name: "short mismatch", left: "short", right: "0123456789abcdef"},
		{name: "long mismatch", left: "0123456789abcdef-extra", right: "0123456789abcdef"},
		{name: "empty values", left: "", right: "", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := constantTimeEqual(test.left, test.right); got != test.want {
				t.Fatalf("constantTimeEqual() = %v, want %v", got, test.want)
			}
		})
	}
}
