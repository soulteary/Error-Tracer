package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRateLimiterBoundsAndSharesOverflowBudget(t *testing.T) {
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(60, 1)
	limiter.maxClients = 2
	limiter.now = func() time.Time { return now }

	limiter.Allow("client-a")
	limiter.Allow("client-b")
	if allowed, retryAfter := limiter.Allow("client-c"); !allowed || retryAfter != 0 {
		t.Fatalf("first overflow client = (%v, %s), want admitted", allowed, retryAfter)
	}
	if len(limiter.buckets) != 2 {
		t.Fatalf("buckets = %d, want bounded at 2", len(limiter.buckets))
	}
	if _, exists := limiter.buckets["client-c"]; exists {
		t.Fatal("overflow client unexpectedly displaced a retained bucket")
	}
	if allowed, retryAfter := limiter.Allow("client-d"); allowed || retryAfter != time.Second {
		t.Fatalf("rotated overflow client = (%v, %s), want shared-budget rejection for 1s", allowed, retryAfter)
	}
	if allowed, _ := limiter.Allow("client-c"); allowed {
		t.Fatal("returning overflow client reset the shared budget")
	}
	if allowed, _ := limiter.Allow("client-a"); allowed {
		t.Fatal("saturated traffic reset an existing client bucket")
	}

	now = now.Add(rateLimitClientTTL)
	if allowed, _ := limiter.Allow("client-c"); !allowed {
		t.Fatal("expired buckets were not reclaimed")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("buckets after sweep = %d, want 1", len(limiter.buckets))
	}
}

func TestRateLimiterChargesWeightedRequestsAtomically(t *testing.T) {
	now := time.Date(2026, time.August, 29, 3, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(60, 3)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.AllowN("client", 3); !allowed {
		t.Fatal("weighted request within the burst was rejected")
	}
	if allowed, retryAfter := limiter.Allow("client"); allowed || retryAfter != time.Second {
		t.Fatalf("post-batch request = (%v, %s), want rejected for 1s", allowed, retryAfter)
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("refilled token was not available")
	}

	other := newRateLimiter(60, 3)
	other.now = func() time.Time { return now }
	if allowed, retryAfter := other.AllowN("client", 4); allowed || retryAfter != rateLimitRetryNever {
		t.Fatalf(
			"request larger than the burst = (%v, %s), want permanent rejection",
			allowed, retryAfter,
		)
	}
	if len(other.buckets) != 0 {
		t.Fatalf("permanently rejected request created a bucket: %#v", other.buckets)
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

func TestIngestBatchConsumesOneTokenPerEvent(t *testing.T) {
	memory := store.NewMemory()
	app := New(Options{
		Store:         memory,
		ProjectID:     "project-a",
		IngestKey:     "0123456789abcdef",
		RatePerMinute: 60,
		RateBurst:     3,
	})
	body := `{"project_key":"0123456789abcdef","events":[` +
		`{"kind":"error","message":"one"},` +
		`{"kind":"error","message":"two"},` +
		`{"kind":"error","message":"three"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:1000"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("batch status = %d, want %d; body = %s", response.Code, http.StatusAccepted, response.Body.String())
	}

	next := eventRequest(http.MethodPost)
	next.RemoteAddr = "192.0.2.10:2000"
	nextResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(nextResponse, next)
	if nextResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("post-batch status = %d, want %d", nextResponse.Code, http.StatusTooManyRequests)
	}
}

func TestIngestBatchRetryAfterIncludesTheWholeAtomicCharge(t *testing.T) {
	now := time.Date(2026, time.August, 30, 20, 0, 0, 0, time.UTC)
	app := New(Options{
		Store:         store.NewMemory(),
		ProjectID:     "project-a",
		IngestKey:     "0123456789abcdef",
		RatePerMinute: 60,
		RateBurst:     3,
	})
	app.requestLimiter.now = func() time.Time { return now }
	app.ingestLimiter.now = func() time.Time { return now }
	body := `{"project_key":"0123456789abcdef","events":[` +
		`{"kind":"error","message":"one"},` +
		`{"kind":"error","message":"two"},` +
		`{"kind":"error","message":"three"}]}`
	submit := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/events/batch", strings.NewReader(body),
		)
		request.RemoteAddr = "192.0.2.10:1000"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	if response := submit(); response.Code != http.StatusAccepted {
		t.Fatalf("initial batch status = %d, want %d", response.Code, http.StatusAccepted)
	}
	rejected := submit()
	if rejected.Code != http.StatusTooManyRequests {
		t.Fatalf("empty-budget batch status = %d, want %d", rejected.Code, http.StatusTooManyRequests)
	}
	if got := rejected.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3 for the complete batch", got)
	}

	now = now.Add(3 * time.Second)
	if response := submit(); response.Code != http.StatusAccepted {
		t.Fatalf(
			"batch after advertised delay = %d, want %d; body = %s",
			response.Code, http.StatusAccepted, response.Body.String(),
		)
	}
}

func TestIngestBatchLargerThanBurstIsPermanentlyRejected(t *testing.T) {
	memory := store.NewMemory()
	app := New(Options{
		Store:         memory,
		ProjectID:     "project-a",
		IngestKey:     "0123456789abcdef",
		RatePerMinute: 60,
		RateBurst:     3,
	})
	body := `{"project_key":"0123456789abcdef","events":[` +
		`{"kind":"error","message":"one"},` +
		`{"kind":"error","message":"two"},` +
		`{"kind":"error","message":"three"},` +
		`{"kind":"error","message":"four"}]}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/events/batch", strings.NewReader(body),
	)
	request.RemoteAddr = "192.0.2.10:1000"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"over-burst batch status = %d, want %d; body = %s",
			response.Code, http.StatusUnprocessableEntity, response.Body.String(),
		)
	}
	if got := response.Header().Get("Retry-After"); got != "" {
		t.Fatalf("over-burst Retry-After = %q, want no retry advice", got)
	}
	var failure errorResponse
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatalf("decode over-burst response: %v", err)
	}
	if failure.Error != "rate_limit_burst_exceeded" {
		t.Fatalf("over-burst error = %q, want rate_limit_burst_exceeded", failure.Error)
	}
	if len(app.ingestLimiter.buckets) != 0 {
		t.Fatal("over-burst batch consumed or allocated an event bucket")
	}
	page, err := memory.ListIssues(context.Background(), "project-a", store.ListOptions{})
	if err != nil {
		t.Fatalf("list issues after over-burst batch: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("over-burst batch stored %d issues, want none", page.Total)
	}
}

func TestInvalidIngestRequestsUseASeparateRequestBudget(t *testing.T) {
	app := New(Options{
		Store:         store.NewMemory(),
		ProjectID:     "project-a",
		IngestKey:     "0123456789abcdef",
		RatePerMinute: 60,
		RateBurst:     1,
	})
	submit := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/events", strings.NewReader(`{"broken":`),
		)
		request.RemoteAddr = "192.0.2.10:1000"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		return response
	}

	if response := submit(); response.Code != http.StatusBadRequest {
		t.Fatalf("first invalid request status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if len(app.ingestLimiter.buckets) != 0 {
		t.Fatal("invalid request consumed the validated-event budget")
	}
	if response := submit(); response.Code != http.StatusTooManyRequests {
		t.Fatalf("second invalid request status = %d, want %d", response.Code, http.StatusTooManyRequests)
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
