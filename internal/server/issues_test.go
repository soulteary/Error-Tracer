package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	"github.com/soulteary/Error-Tracer/internal/store"
)

const testAdminToken = "0123456789abcdefghijklmn"
const testPreviousAdminToken = "previous-admin-token-1234"

func TestListIssuesRequiresAdminToken(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing"},
		{name: "wrong scheme", authorization: "Basic " + testAdminToken},
		{name: "wrong token", authorization: "Bearer wrong-token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()

			newTestServer().Handler().ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if response.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("WWW-Authenticate header is empty")
			}
		})
	}
}

func TestListIssuesAcceptsCurrentAndPreviousAdminTokens(t *testing.T) {
	app := New(Options{
		Store:              store.NewMemory(),
		ProjectID:          "project-a",
		IngestKey:          "0123456789abcdef",
		AdminToken:         testAdminToken,
		PreviousAdminToken: testPreviousAdminToken,
	})

	for _, token := range []string{testAdminToken, testPreviousAdminToken} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("token %q: status = %d, want %d", token, response.Code, http.StatusOK)
		}
	}
}

func TestListIssuesReturnsBoundedPage(t *testing.T) {
	memory := store.NewMemory()
	base := time.Date(2026, time.August, 29, 2, 0, 0, 0, time.UTC)
	for index, message := range []string{"first", "second", "third"} {
		captured := event.Event{
			Kind:       event.KindError,
			Message:    message,
			ReceivedAt: base.Add(time.Duration(index) * time.Minute),
		}
		if _, err := memory.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record issue: %v", err)
		}
	}
	app := New(Options{
		Store: memory, ProjectID: "project-a", IngestKey: "0123456789abcdef", AdminToken: testAdminToken,
	})
	request := authorizedRequest(http.MethodGet, "/api/v1/issues?limit=1&offset=1")
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var page store.IssuePage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 3 || page.Limit != 1 || page.Offset != 1 || len(page.Issues) != 1 {
		t.Fatalf("page = %#v", page)
	}
	if page.Issues[0].Message != "second" {
		t.Fatalf("message = %q, want second", page.Issues[0].Message)
	}
}

func TestListIssuesRejectsInvalidPagination(t *testing.T) {
	for _, target := range []string{
		"/api/v1/issues?limit=0",
		"/api/v1/issues?limit=101",
		"/api/v1/issues?limit=nope",
		"/api/v1/issues?limit=1&limit=2",
		"/api/v1/issues?offset=-1",
		"/api/v1/issues?offset=100001",
	} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			newTestServer().Handler().ServeHTTP(response, authorizedRequest(http.MethodGet, target))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestListIssuesFiltersByStatus(t *testing.T) {
	memory := store.NewMemory()
	base := time.Date(2026, time.August, 29, 2, 0, 0, 0, time.UTC)
	openEvent := event.Event{
		Kind: event.KindError, Message: "open failure", ReceivedAt: base,
	}
	if _, err := memory.Record(context.Background(), "project-a", openEvent); err != nil {
		t.Fatalf("record open issue: %v", err)
	}
	resolvedEvent := event.Event{
		Kind: event.KindError, Message: "resolved failure", ReceivedAt: base.Add(time.Minute),
	}
	resolved, err := memory.Record(context.Background(), "project-a", resolvedEvent)
	if err != nil {
		t.Fatalf("record resolved issue: %v", err)
	}
	if _, err := memory.SetIssueStatus(
		context.Background(), "project-a", resolved.Fingerprint, store.IssueStatusResolved,
	); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}
	app := New(Options{
		Store: memory, ProjectID: "project-a", IngestKey: "0123456789abcdef", AdminToken: testAdminToken,
	})

	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, authorizedRequest(
		http.MethodGet, "/api/v1/issues?status=resolved",
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var page store.IssuePage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 1 || len(page.Issues) != 1 ||
		page.Issues[0].Status != store.IssueStatusResolved {
		t.Fatalf("page = %#v, want one resolved issue", page)
	}

	for _, target := range []string{
		"/api/v1/issues?status=closed",
		"/api/v1/issues?status=open&status=resolved",
	} {
		invalid := httptest.NewRecorder()
		app.Handler().ServeHTTP(invalid, authorizedRequest(http.MethodGet, target))
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", target, invalid.Code, http.StatusBadRequest)
		}
		var failure errorResponse
		if err := json.NewDecoder(invalid.Body).Decode(&failure); err != nil {
			t.Fatalf("decode invalid status response: %v", err)
		}
		if failure.Error != "invalid_status" || failure.Field != "status" {
			t.Fatalf("failure = %#v, want invalid_status for status", failure)
		}
	}
}

func TestGetIssue(t *testing.T) {
	memory := store.NewMemory()
	captured := event.Event{Kind: event.KindError, Message: "boom", ReceivedAt: time.Now().UTC()}
	stored, err := memory.Record(context.Background(), "project-a", captured)
	if err != nil {
		t.Fatalf("record issue: %v", err)
	}
	app := New(Options{
		Store: memory, ProjectID: "project-a", IngestKey: "0123456789abcdef", AdminToken: testAdminToken,
	})
	request := authorizedRequest(http.MethodGet, "/api/v1/issues/"+stored.Fingerprint)
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var result issueResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if result.Issue.Fingerprint != stored.Fingerprint {
		t.Fatalf("fingerprint = %q, want %q", result.Issue.Fingerprint, stored.Fingerprint)
	}
}

func TestGetIssueRejectsInvalidOrMissingFingerprint(t *testing.T) {
	tests := []struct {
		fingerprint string
		wantStatus  int
	}{
		{fingerprint: "not-a-fingerprint", wantStatus: http.StatusBadRequest},
		{fingerprint: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", wantStatus: http.StatusBadRequest},
		{fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		request := authorizedRequest(http.MethodGet, "/api/v1/issues/"+test.fingerprint)
		response := httptest.NewRecorder()
		newTestServer().Handler().ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Fatalf("fingerprint %q: status = %d, want %d", test.fingerprint, response.Code, test.wantStatus)
		}
	}
}

func TestIssuesAPIRejectsUnconfiguredServer(t *testing.T) {
	request := authorizedRequest(http.MethodGet, "/api/v1/issues")
	response := httptest.NewRecorder()
	New(Options{}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestUpdateIssueStatus(t *testing.T) {
	memory := store.NewMemory()
	captured := event.Event{Kind: event.KindError, Message: "boom", ReceivedAt: time.Now().UTC()}
	stored, err := memory.Record(context.Background(), "project-a", captured)
	if err != nil {
		t.Fatalf("record issue: %v", err)
	}
	app := New(Options{
		Store: memory, ProjectID: "project-a", IngestKey: "0123456789abcdef", AdminToken: testAdminToken,
	})
	request := authorizedRequest(http.MethodPatch, "/api/v1/issues/"+stored.Fingerprint)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Body = io.NopCloser(strings.NewReader(`{"status":"resolved"}`))
	response := httptest.NewRecorder()

	app.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	persisted, err := memory.GetIssue(context.Background(), "project-a", stored.Fingerprint)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if persisted.Status != store.IssueStatusResolved {
		t.Fatalf("status = %q, want %q", persisted.Status, store.IssueStatusResolved)
	}
}

func TestUpdateIssueRejectsInvalidRequests(t *testing.T) {
	validFingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	oversized := append([]byte(`{"status":"`), bytes.Repeat([]byte("x"), maxIssueUpdateBodySize+1)...)
	oversized = append(oversized, []byte(`"}`)...)
	tests := []struct {
		name        string
		fingerprint string
		contentType string
		body        []byte
		wantStatus  int
	}{
		{name: "fingerprint", fingerprint: "invalid", contentType: "application/json", body: []byte(`{"status":"open"}`), wantStatus: http.StatusBadRequest},
		{name: "media type", fingerprint: validFingerprint, contentType: "text/plain", body: []byte(`{"status":"open"}`), wantStatus: http.StatusUnsupportedMediaType},
		{name: "JSON", fingerprint: validFingerprint, contentType: "application/json", body: []byte(`{`), wantStatus: http.StatusBadRequest},
		{name: "unknown field", fingerprint: validFingerprint, contentType: "application/json", body: []byte(`{"other":true}`), wantStatus: http.StatusBadRequest},
		{name: "multiple values", fingerprint: validFingerprint, contentType: "application/json", body: []byte(`{"status":"open"}{}`), wantStatus: http.StatusBadRequest},
		{name: "status", fingerprint: validFingerprint, contentType: "application/json", body: []byte(`{"status":"closed"}`), wantStatus: http.StatusUnprocessableEntity},
		{name: "too large", fingerprint: validFingerprint, contentType: "application/json", body: oversized, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "missing", fingerprint: validFingerprint, contentType: "application/json", body: []byte(`{"status":"open"}`), wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := authorizedRequest(http.MethodPatch, "/api/v1/issues/"+test.fingerprint)
			request.Header.Set("Content-Type", test.contentType)
			request.Body = io.NopCloser(bytes.NewReader(test.body))
			response := httptest.NewRecorder()

			newTestServer().Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func authorizedRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+testAdminToken)
	return request
}
