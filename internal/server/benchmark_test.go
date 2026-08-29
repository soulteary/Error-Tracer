package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	"github.com/soulteary/Error-Tracer/internal/store"
)

func BenchmarkIngestHandler(b *testing.B) {
	benchmarks := []struct {
		name      string
		path      string
		batchSize int
	}{
		{name: "Single", path: "/api/v1/events", batchSize: 1},
		{name: "Batch_010", path: "/api/v1/events/batch", batchSize: 10},
		{name: "Batch_100", path: "/api/v1/events/batch", batchSize: 100},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			body := benchmarkIngestBody(b, benchmark.batchSize, benchmark.path)
			app := New(Options{
				Store:         store.NewMemory(),
				ProjectID:     "benchmark",
				IngestKey:     "0123456789abcdef",
				RatePerMinute: 1_000_000_000,
				RateBurst:     1_000_000_000,
			})
			app.now = func() time.Time {
				return time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
			}
			app.newID = func() (string, error) { return "evt_benchmark", nil }

			b.ReportAllocs()
			b.ReportMetric(float64(benchmark.batchSize), "events/op")
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for range b.N {
				request := httptest.NewRequest(http.MethodPost, benchmark.path, bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				app.Handler().ServeHTTP(response, request)
				if response.Code != http.StatusAccepted {
					b.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
				}
			}
		})
	}
}

func benchmarkIngestBody(b *testing.B, batchSize int, path string) []byte {
	b.Helper()
	events := make([]event.Event, batchSize)
	for index := range events {
		events[index] = event.Event{
			Kind:        event.KindError,
			Message:     fmt.Sprintf("benchmark failure %03d", index),
			Stack:       "Error: benchmark failure\n    at run (https://benchmark.invalid/app.js:42:7)",
			SourceURL:   "https://benchmark.invalid/app.js",
			PageURL:     "https://benchmark.invalid/checkout",
			Line:        42,
			Column:      7,
			Release:     "benchmark-1",
			Environment: "benchmark",
			Tags:        map[string]string{"suite": "http"},
		}
	}

	var payload any
	if path == "/api/v1/events" {
		payload = ingestRequest{ProjectKey: "0123456789abcdef", Event: events[0]}
	} else {
		payload = batchIngestRequest{ProjectKey: "0123456789abcdef", Events: events}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		b.Fatalf("marshal benchmark body: %v", err)
	}
	return body
}
