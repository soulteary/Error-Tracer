package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
)

func BenchmarkRecordBatch(b *testing.B) {
	stores := []struct {
		name string
		open func(*testing.B) Store
	}{
		{
			name: "Memory",
			open: func(*testing.B) Store {
				return NewMemory()
			},
		},
		{
			name: "SQLite",
			open: func(b *testing.B) Store {
				database, err := OpenSQLite(
					context.Background(), filepath.Join(b.TempDir(), "benchmark.db"),
				)
				if err != nil {
					b.Fatalf("open SQLite: %v", err)
				}
				b.Cleanup(func() { _ = database.Close() })
				return database
			},
		},
	}

	for _, implementation := range stores {
		b.Run(implementation.name, func(b *testing.B) {
			for _, batchSize := range []int{1, 10, 100} {
				b.Run(fmt.Sprintf("Batch_%03d", batchSize), func(b *testing.B) {
					issueStore := implementation.open(b)
					captured := benchmarkEvents(batchSize)
					b.ReportAllocs()
					b.ReportMetric(float64(batchSize), "events/op")
					b.ResetTimer()
					for range b.N {
						if _, err := issueStore.RecordBatch(
							context.Background(), "benchmark", captured,
						); err != nil {
							b.Fatalf("record batch: %v", err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkListIssues(b *testing.B) {
	stores := []struct {
		name string
		open func(*testing.B) Store
	}{
		{
			name: "Memory",
			open: func(*testing.B) Store {
				return NewMemory()
			},
		},
		{
			name: "SQLite",
			open: func(b *testing.B) Store {
				database, err := OpenSQLite(
					context.Background(), filepath.Join(b.TempDir(), "benchmark.db"),
				)
				if err != nil {
					b.Fatalf("open SQLite: %v", err)
				}
				b.Cleanup(func() { _ = database.Close() })
				return database
			},
		},
	}

	for _, implementation := range stores {
		b.Run(implementation.name, func(b *testing.B) {
			issueStore := implementation.open(b)
			for offset := 0; offset < 1_000; offset += 100 {
				captured := benchmarkEventsAt(100, offset)
				if _, err := issueStore.RecordBatch(
					context.Background(), "benchmark", captured,
				); err != nil {
					b.Fatalf("seed issues: %v", err)
				}
			}
			options := ListOptions{Limit: 50, Offset: 475}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				page, err := issueStore.ListIssues(context.Background(), "benchmark", options)
				if err != nil {
					b.Fatalf("list issues: %v", err)
				}
				if len(page.Issues) != 50 || page.Total != 1_000 {
					b.Fatalf("unexpected page: %d of %d", len(page.Issues), page.Total)
				}
			}
		})
	}
}

func benchmarkEvents(count int) []event.Event {
	return benchmarkEventsAt(count, 0)
}

func benchmarkEventsAt(count, offset int) []event.Event {
	receivedAt := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	captured := make([]event.Event, count)
	for index := range captured {
		number := offset + index
		captured[index] = event.Event{
			ID:          fmt.Sprintf("evt_benchmark_%06d", number),
			Kind:        event.KindError,
			Message:     fmt.Sprintf("benchmark failure %06d", number),
			Stack:       fmt.Sprintf("Error: benchmark failure %06d\n    at run (https://benchmark.invalid/app.js:42:7)", number),
			SourceURL:   "https://benchmark.invalid/app.js",
			PageURL:     "https://benchmark.invalid/checkout",
			Line:        42,
			Column:      7,
			ReceivedAt:  receivedAt.Add(time.Duration(number) * time.Millisecond),
			Release:     "benchmark-1",
			Environment: "benchmark",
			UserAgent:   "error-tracer-benchmark",
			Tags:        map[string]string{"suite": "store"},
		}
	}
	return captured
}
