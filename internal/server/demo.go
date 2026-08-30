package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/soulteary/Error-Tracer/internal/buildinfo"
	"github.com/soulteary/Error-Tracer/internal/event"
	"github.com/soulteary/Error-Tracer/internal/store"
)

const demoProjectID = "error-tracer-demo"

type publicMetadataResponse struct {
	DemoMode bool   `json:"demo_mode"`
	DemoOnly bool   `json:"demo_only,omitempty"`
	Version  string `json:"version"`
}

type demoFixture struct {
	event       event.Event
	occurrences int
	firstAgo    time.Duration
	lastAgo     time.Duration
	status      store.IssueStatus
}

func (s *Server) publicMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicMetadataResponse{
		DemoMode: s.demoStore != nil,
		DemoOnly: s.demoOnly,
		Version:  buildinfo.Current().Version,
	})
}

func (s *Server) listDemoIssues(w http.ResponseWriter, request *http.Request) {
	if s.demoStore == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "demo_unavailable"})
		return
	}
	options, err := parseListOptions(request)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStatus) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_status", Field: "status"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_pagination"})
		return
	}

	page, err := s.demoStore.ListIssues(request.Context(), demoProjectID, options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeIssuePage(w, page)
}

func (s *Server) getDemoIssue(w http.ResponseWriter, request *http.Request) {
	if s.demoStore == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "demo_unavailable"})
		return
	}
	fingerprint := request.PathValue("fingerprint")
	if !validFingerprint(fingerprint) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_fingerprint"})
		return
	}

	issue, err := s.demoStore.GetIssue(request.Context(), demoProjectID, fingerprint)
	if errors.Is(err, store.ErrIssueNotFound) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "issue_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, issueResponse{Issue: issue})
}

func newDemoStore(now time.Time) store.Store {
	memory := store.NewMemory()
	if err := seedDemoStore(memory, now); err != nil {
		panic(fmt.Sprintf("seed demo store: %v", err))
	}
	return memory
}

func seedDemoStore(memory *store.Memory, now time.Time) error {
	fixtures := []demoFixture{
		{
			event: event.Event{
				Kind:        event.KindError,
				Message:     "Cannot read properties of undefined (reading 'total')",
				Stack:       "TypeError: Cannot read properties of undefined (reading 'total')\n    at calculateTotal (https://shop.example.com/assets/checkout.js:184:17)\n    at submitOrder (https://shop.example.com/assets/checkout.js:241:9)",
				SourceURL:   "https://shop.example.com/assets/checkout.js",
				PageURL:     "https://shop.example.com/checkout",
				Line:        184,
				Column:      17,
				Release:     "web-2026.08.29.2",
				Environment: "production",
				UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/139.0 Safari/537.36",
				Tags:        map[string]string{"feature": "checkout", "region": "eu-west"},
			},
			occurrences: 43,
			firstAgo:    72 * time.Hour,
			lastAgo:     2 * time.Minute,
			status:      store.IssueStatusOpen,
		},
		{
			event: event.Event{
				Kind:        event.KindUnhandledRejection,
				Message:     "Payment request timed out after 10000ms",
				Stack:       "Error: Payment request timed out after 10000ms\n    at requestPayment (https://shop.example.com/assets/payments.js:92:11)",
				SourceURL:   "https://shop.example.com/assets/payments.js",
				PageURL:     "https://shop.example.com/checkout/payment",
				Line:        92,
				Column:      11,
				Release:     "web-2026.08.29.2",
				Environment: "production",
				UserAgent:   "Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 Chrome/139.0 Mobile Safari/537.36",
				Tags:        map[string]string{"provider": "example-pay", "region": "ap-south"},
			},
			occurrences: 17,
			firstAgo:    36 * time.Hour,
			lastAgo:     18 * time.Minute,
			status:      store.IssueStatusResolved,
		},
		{
			event: event.Event{
				Kind:        event.KindResourceError,
				Message:     "Failed to load JavaScript chunk 847",
				SourceURL:   "https://cdn.example.com/assets/chunk-847.js",
				PageURL:     "https://shop.example.com/account/orders",
				Release:     "web-2026.08.29.2",
				Environment: "production",
				UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 Version/19.0 Mobile Safari/604.1",
				Tags:        map[string]string{"cdn": "edge-a", "route": "orders"},
			},
			occurrences: 9,
			firstAgo:    15 * time.Hour,
			lastAgo:     41 * time.Minute,
			status:      store.IssueStatusOpen,
		},
		{
			event: event.Event{
				Kind:        event.KindError,
				Message:     "ResizeObserver loop completed with undelivered notifications",
				SourceURL:   "https://shop.example.com/assets/catalog.js",
				PageURL:     "https://shop.example.com/catalog",
				Line:        318,
				Column:      5,
				Release:     "web-2026.08.28.7",
				Environment: "production",
				UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/139.0 Safari/537.36",
				Tags:        map[string]string{"component": "product-grid"},
			},
			occurrences: 31,
			firstAgo:    7 * 24 * time.Hour,
			lastAgo:     2 * time.Hour,
			status:      store.IssueStatusIgnored,
		},
		{
			event: event.Event{
				Kind:        event.KindError,
				Message:     "Failed to fetch recommendations",
				Stack:       "TypeError: Failed to fetch\n    at loadRecommendations (https://shop.example.com/assets/home.js:77:23)",
				SourceURL:   "https://shop.example.com/assets/home.js",
				PageURL:     "https://shop.example.com/",
				Line:        77,
				Column:      23,
				Release:     "web-2026.08.29.2",
				Environment: "staging",
				UserAgent:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Firefox/141.0",
				Tags:        map[string]string{"experiment": "home-personalization"},
			},
			occurrences: 6,
			firstAgo:    26 * time.Hour,
			lastAgo:     5 * time.Hour,
			status:      store.IssueStatusOpen,
		},
	}

	captured := make([]event.Event, 0, 106)
	for fixtureIndex, fixture := range fixtures {
		span := fixture.firstAgo - fixture.lastAgo
		for occurrence := range fixture.occurrences {
			receivedAt := now.Add(-fixture.firstAgo)
			if fixture.occurrences > 1 {
				receivedAt = receivedAt.Add(time.Duration(occurrence) * span / time.Duration(fixture.occurrences-1))
			}
			occurredAt := receivedAt.Add(-2 * time.Second)
			item := fixture.event
			item.ID = fmt.Sprintf("evt_demo_%02d_%03d", fixtureIndex+1, occurrence+1)
			item.OccurredAt = &occurredAt
			item.ReceivedAt = receivedAt
			captured = append(captured, item)
		}
	}

	if _, err := memory.RecordBatch(context.Background(), demoProjectID, captured); err != nil {
		return err
	}
	for _, fixture := range fixtures {
		if fixture.status == store.IssueStatusOpen {
			continue
		}
		if _, err := memory.SetIssueStatus(
			context.Background(), demoProjectID, fixture.event.Fingerprint(), fixture.status,
		); err != nil {
			return err
		}
	}
	return nil
}
