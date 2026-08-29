package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	"github.com/soulteary/Error-Tracer/internal/store"
)

const testAdminToken = "0123456789abcdefghijklmn"

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

func authorizedRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+testAdminToken)
	return request
}
