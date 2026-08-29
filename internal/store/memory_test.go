package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
)

func TestMemoryRecordAggregatesMatchingEvents(t *testing.T) {
	memory := NewMemory()
	firstTime := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	secondTime := firstTime.Add(time.Minute)
	first := testEvent(firstTime)
	second := testEvent(secondTime)
	second.Release = "2.0.0"

	if _, err := memory.Record(context.Background(), "project-a", first); err != nil {
		t.Fatalf("record first event: %v", err)
	}
	issue, err := memory.Record(context.Background(), "project-a", second)
	if err != nil {
		t.Fatalf("record second event: %v", err)
	}

	if issue.Occurrences != 2 {
		t.Fatalf("Occurrences = %d, want 2", issue.Occurrences)
	}
	if !issue.FirstSeen.Equal(firstTime) {
		t.Fatalf("FirstSeen = %s, want %s", issue.FirstSeen, firstTime)
	}
	if !issue.LastSeen.Equal(secondTime) {
		t.Fatalf("LastSeen = %s, want %s", issue.LastSeen, secondTime)
	}
	if issue.LastEvent.Release != "2.0.0" {
		t.Fatalf("LastEvent.Release = %q, want latest event", issue.LastEvent.Release)
	}
}

func TestMemorySeparatesProjects(t *testing.T) {
	memory := NewMemory()
	captured := testEvent(time.Now())

	for _, projectID := range []string{"project-a", "project-b"} {
		if _, err := memory.Record(context.Background(), projectID, captured); err != nil {
			t.Fatalf("record %s: %v", projectID, err)
		}
	}

	for _, projectID := range []string{"project-a", "project-b"} {
		page, err := memory.ListIssues(context.Background(), projectID, ListOptions{})
		if err != nil {
			t.Fatalf("list %s: %v", projectID, err)
		}
		if page.Total != 1 || page.Issues[0].ProjectID != projectID {
			t.Fatalf("page for %s = %#v, want one isolated issue", projectID, page)
		}
	}
}

func TestMemoryListIssuesIsBoundedAndOrdered(t *testing.T) {
	memory := NewMemory()
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		captured := testEvent(base.Add(time.Duration(index) * time.Minute))
		captured.Message = fmt.Sprintf("failure-%d", index)
		if _, err := memory.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record event %d: %v", index, err)
		}
	}

	page, err := memory.ListIssues(context.Background(), "project-a", ListOptions{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if page.Total != 3 || len(page.Issues) != 1 {
		t.Fatalf("page = %#v, want one of three issues", page)
	}
	if page.Issues[0].Message != "failure-1" {
		t.Fatalf("message = %q, want second-most-recent issue", page.Issues[0].Message)
	}
}

func TestMemoryRecordIsConcurrencySafe(t *testing.T) {
	memory := NewMemory()
	captured := testEvent(time.Now())
	const workers = 100

	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, workers)
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := memory.Record(context.Background(), "project-a", captured)
			errorsChannel <- err
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("record event: %v", err)
		}
	}

	issue, err := memory.GetIssue(context.Background(), "project-a", captured.Fingerprint())
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.Occurrences != workers {
		t.Fatalf("Occurrences = %d, want %d", issue.Occurrences, workers)
	}
}

func TestMemoryReturnsDefensiveCopies(t *testing.T) {
	memory := NewMemory()
	captured := testEvent(time.Now())
	captured.Tags = map[string]string{"feature": "checkout"}
	issue, err := memory.Record(context.Background(), "project-a", captured)
	if err != nil {
		t.Fatalf("record event: %v", err)
	}

	issue.LastEvent.Tags["feature"] = "modified"
	stored, err := memory.GetIssue(context.Background(), "project-a", captured.Fingerprint())
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if stored.LastEvent.Tags["feature"] != "checkout" {
		t.Fatalf("stored tags were mutated through returned issue: %#v", stored.LastEvent.Tags)
	}
}

func TestMemorySetIssueStatus(t *testing.T) {
	memory := NewMemory()
	captured := testEvent(time.Now())
	stored, err := memory.Record(context.Background(), "project-a", captured)
	if err != nil {
		t.Fatalf("record event: %v", err)
	}

	updated, err := memory.SetIssueStatus(
		context.Background(), "project-a", stored.Fingerprint, IssueStatusResolved,
	)
	if err != nil {
		t.Fatalf("set issue status: %v", err)
	}
	if updated.Status != IssueStatusResolved {
		t.Fatalf("status = %q, want %q", updated.Status, IssueStatusResolved)
	}
	persisted, err := memory.GetIssue(context.Background(), "project-a", stored.Fingerprint)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if persisted.Status != IssueStatusResolved {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, IssueStatusResolved)
	}

	if _, err := memory.SetIssueStatus(context.Background(), "project-a", stored.Fingerprint, "closed"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidStatus", err)
	}
	if _, err := memory.SetIssueStatus(context.Background(), "project-a", "missing", IssueStatusIgnored); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue error = %v, want ErrIssueNotFound", err)
	}
}

func TestMemoryRejectsInvalidInputAndCancelledContext(t *testing.T) {
	memory := NewMemory()
	captured := testEvent(time.Now())

	if _, err := memory.Record(context.Background(), "", captured); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("empty project error = %v, want ErrProjectRequired", err)
	}
	captured.ReceivedAt = time.Time{}
	if _, err := memory.Record(context.Background(), "project-a", captured); !errors.Is(err, ErrReceivedAtEmpty) {
		t.Fatalf("empty received_at error = %v, want ErrReceivedAtEmpty", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := memory.ListIssues(ctx, "project-a", ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v, want context.Canceled", err)
	}
}

func testEvent(receivedAt time.Time) event.Event {
	return event.Event{
		ID:         "event-id",
		Kind:       event.KindError,
		Message:    "boom",
		SourceURL:  "https://example.com/app.js",
		Line:       10,
		Column:     2,
		ReceivedAt: receivedAt,
	}
}
