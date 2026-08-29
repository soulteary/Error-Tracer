package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/store"
)

func TestRateLimiterRefillsAndCapsTokens(t *testing.T) {
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(60, 2)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("first event was rejected")
	}
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("second event was rejected")
	}
	if allowed, retryAfter := limiter.Allow("client"); allowed || retryAfter != time.Second {
		t.Fatalf("third event = (%v, %s), want rejected for 1s", allowed, retryAfter)
	}

	now = now.Add(500 * time.Millisecond)
	if allowed, retryAfter := limiter.Allow("client"); allowed || retryAfter != 500*time.Millisecond {
		t.Fatalf("half-refilled event = (%v, %s), want rejected for 500ms", allowed, retryAfter)
	}
	now = now.Add(500 * time.Millisecond)
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("refilled event was rejected")
	}

	now = now.Add(10 * time.Second)
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("first capped event was rejected")
	}
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("second capped event was rejected")
	}
	if allowed, _ := limiter.Allow("client"); allowed {
		t.Fatal("bucket exceeded its configured burst")
	}
}

func TestRateLimiterSeparatesClients(t *testing.T) {
	limiter := newRateLimiter(60, 1)
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.Allow("client-a"); !allowed {
		t.Fatal("client-a first event was rejected")
	}
	if allowed, _ := limiter.Allow("client-a"); allowed {
		t.Fatal("client-a exceeded its burst")
	}
	if allowed, _ := limiter.Allow("client-b"); !allowed {
		t.Fatal("client-b was affected by client-a")
	}
}

func TestRateLimiterBoundsAndExpiresClientBuckets(t *testing.T) {
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(60, 1)
	limiter.maxClients = 2
	limiter.now = func() time.Time { return now }

	limiter.Allow("client-a")
	limiter.Allow("client-b")
	if allowed, retryAfter := limiter.Allow("client-c"); allowed || retryAfter != time.Minute {
		t.Fatalf("overflow client = (%v, %s), want rejected for 1m", allowed, retryAfter)
	}
	if len(limiter.buckets) != 2 {
		t.Fatalf("buckets = %d, want bounded at 2", len(limiter.buckets))
	}

	now = now.Add(rateLimitClientTTL)
	if allowed, _ := limiter.Allow("client-c"); !allowed {
		t.Fatal("expired buckets were not reclaimed")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("buckets after sweep = %d, want 1", len(limiter.buckets))
	}
}

func TestClientAddressUsesOnlyDirectPeer(t *testing.T) {
	tests := map[string]string{
		"192.0.2.10:4321":     "192.0.2.10",
		"192.0.2.10":          "192.0.2.10",
		"[2001:db8::1]:4321":  "2001:db8::1",
		"2001:db8::1":         "2001:db8::1",
		"proxy.example:4321":  "unknown",
		"malformed forwarded": "unknown",
		"":                    "unknown",
	}

	for input, want := range tests {
		if got := clientAddress(input); got != want {
			t.Errorf("clientAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRetryAfterHeaderRoundsUp(t *testing.T) {
	tests := map[time.Duration]string{
		0:                      "1",
		100 * time.Millisecond: "1",
		time.Second:            "1",
		time.Second + 1:        "2",
	}
	for input, want := range tests {
		if got := retryAfterHeader(input); got != want {
			t.Errorf("retryAfterHeader(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestIngestRateLimitUsesRemoteAddress(t *testing.T) {
	memory := store.NewMemory()
	app := New(Options{
		Store:         memory,
		ProjectID:     "project-a",
		IngestKey:     "0123456789abcdef",
		RatePerMinute: 60,
		RateBurst:     1,
	})

	first := eventRequest(http.MethodPost)
	first.RemoteAddr = "192.0.2.10:1000"
	first.Header.Set("X-Forwarded-For", "198.51.100.1")
	firstResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d", firstResponse.Code, http.StatusAccepted)
	}

	second := eventRequest(http.MethodPost)
	second.RemoteAddr = "192.0.2.10:2000"
	second.Header.Set("X-Forwarded-For", "203.0.113.200")
	secondResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d; body = %s", secondResponse.Code, http.StatusTooManyRequests, secondResponse.Body.String())
	}
	if got := secondResponse.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
	var failure errorResponse
	if err := json.NewDecoder(secondResponse.Body).Decode(&failure); err != nil {
		t.Fatalf("decode rate-limit response: %v", err)
	}
	if failure.Error != "rate_limited" {
		t.Fatalf("error = %q, want rate_limited", failure.Error)
	}

	third := eventRequest(http.MethodPost)
	third.RemoteAddr = "192.0.2.11:1000"
	thirdResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(thirdResponse, third)
	if thirdResponse.Code != http.StatusAccepted {
		t.Fatalf("different peer status = %d, want %d", thirdResponse.Code, http.StatusAccepted)
	}

	page, err := memory.ListIssues(context.Background(), "project-a", store.ListOptions{})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if page.Total != 1 || page.Issues[0].Occurrences != 2 {
		t.Fatalf("stored page = %#v, want one issue with two accepted events", page)
	}
}

func TestPreflightDoesNotConsumeIngestBudget(t *testing.T) {
	app := New(Options{
		Store:          store.NewMemory(),
		ProjectID:      "project-a",
		IngestKey:      "0123456789abcdef",
		AllowedOrigins: []string{"https://app.example.com"},
		RatePerMinute:  60,
		RateBurst:      1,
	})

	preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/events", nil)
	preflight.RemoteAddr = "192.0.2.10:1000"
	preflight.Header.Set("Origin", "https://app.example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflightResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflightResponse.Code, http.StatusNoContent)
	}

	submission := eventRequest(http.MethodPost)
	submission.RemoteAddr = "192.0.2.10:2000"
	submission.Header.Set("Origin", "https://app.example.com")
	submissionResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(submissionResponse, submission)
	if submissionResponse.Code != http.StatusAccepted {
		t.Fatalf("submission status = %d, want %d", submissionResponse.Code, http.StatusAccepted)
	}
}
