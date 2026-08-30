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

func TestMemoryRecordBatchAggregatesAndValidatesBeforeWriting(t *testing.T) {
	memory := NewMemory()
	firstTime := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	first := testEvent(firstTime)
	second := testEvent(firstTime.Add(time.Minute))
	second.Release = "2.0.0"

	issues, err := memory.RecordBatch(
		context.Background(), "project-a", []event.Event{first, second},
	)
	if err != nil {
		t.Fatalf("record batch: %v", err)
	}
	if len(issues) != 2 || issues[0].Occurrences != 2 || issues[1].Occurrences != 2 {
		t.Fatalf("batch issues = %#v, want two views of the final aggregate", issues)
	}
	if issues[1].LastEvent.Release != "2.0.0" {
		t.Fatalf("last event = %#v, want second batch event", issues[1].LastEvent)
	}

	invalid := testEvent(firstTime.Add(2 * time.Minute))
	invalid.Message = "must not be stored"
	invalid.ReceivedAt = time.Time{}
	if _, err := memory.RecordBatch(
		context.Background(), "project-a", []event.Event{testEvent(firstTime), invalid},
	); !errors.Is(err, ErrReceivedAtEmpty) {
		t.Fatalf("invalid batch error = %v, want ErrReceivedAtEmpty", err)
	}
	page, err := memory.ListIssues(context.Background(), "project-a", ListOptions{})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if page.Total != 1 || page.Issues[0].Occurrences != 2 {
		t.Fatalf("page after invalid batch = %#v, want the original two events only", page)
	}
	if _, err := memory.RecordBatch(
		context.Background(), "project-a", nil,
	); !errors.Is(err, ErrEventsRequired) {
		t.Fatalf("empty batch error = %v, want ErrEventsRequired", err)
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

func TestMemoryListIssuesDoesNotRetainUnrelatedProjectCapacity(t *testing.T) {
	memory := NewMemory()
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	for index := range 100 {
		captured := testEvent(base.Add(time.Duration(index) * time.Minute))
		captured.Message = fmt.Sprintf("unrelated-%d", index)
		if _, err := memory.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record unrelated event %d: %v", index, err)
		}
	}

	page, err := memory.ListIssues(context.Background(), "project-b", ListOptions{})
	if err != nil {
		t.Fatalf("list empty project: %v", err)
	}
	if len(page.Issues) != 0 || cap(page.Issues) != 0 {
		t.Fatalf(
			"empty project issues length/capacity = %d/%d, want 0/0",
			len(page.Issues), cap(page.Issues),
		)
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

func TestMemoryListIssuesContinuesFromCursor(t *testing.T) {
	memory := NewMemory()
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		captured := testEvent(base.Add(time.Duration(index) * time.Minute))
		captured.Message = fmt.Sprintf("cursor-%d", index)
		if _, err := memory.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record event %d: %v", index, err)
		}
	}

	first, err := memory.ListIssues(context.Background(), "project-a", ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Issues) != 2 || first.Next == nil {
		t.Fatalf("first page = %#v, want two issues and a cursor", first)
	}
	second, err := memory.ListIssues(
		context.Background(), "project-a", ListOptions{Limit: 2, After: first.Next},
	)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if second.Total != 3 || len(second.Issues) != 1 || second.Next != nil {
		t.Fatalf("second page = %#v, want final issue without cursor", second)
	}
	if second.Issues[0].Message != "cursor-0" {
		t.Fatalf("second page message = %q, want cursor-0", second.Issues[0].Message)
	}
}

func TestMemoryListIssuesFiltersByStatus(t *testing.T) {
	memory := NewMemory()
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	openEvent := testEvent(base)
	openEvent.Message = "open failure"
	if _, err := memory.Record(context.Background(), "project-a", openEvent); err != nil {
		t.Fatalf("record open issue: %v", err)
	}
	resolvedEvent := testEvent(base.Add(time.Minute))
	resolvedEvent.Message = "resolved failure"
	resolved, err := memory.Record(context.Background(), "project-a", resolvedEvent)
	if err != nil {
		t.Fatalf("record resolved issue: %v", err)
	}
	if _, err := memory.SetIssueStatus(
		context.Background(), "project-a", resolved.Fingerprint, IssueStatusResolved,
	); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}

	page, err := memory.ListIssues(
		context.Background(), "project-a", ListOptions{Status: IssueStatusResolved},
	)
	if err != nil {
		t.Fatalf("list resolved issues: %v", err)
	}
	if page.Total != 1 || len(page.Issues) != 1 ||
		page.Issues[0].Message != "resolved failure" {
		t.Fatalf("resolved page = %#v, want only the resolved issue", page)
	}
	if _, err := memory.ListIssues(
		context.Background(), "project-a", ListOptions{Status: "closed"},
	); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidStatus", err)
	}
}

func TestMemoryListIssueEventsIsBoundedProjectScopedAndCursorPaginated(t *testing.T) {
	memory := NewMemoryWithOptions(MemoryOptions{MaxEventsPerIssue: 3})
	base := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)
	events := historyEvents(base, 5, "bounded history")
	events[3].ReceivedAt = events[4].ReceivedAt
	for _, captured := range events {
		if _, err := memory.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record project-a history: %v", err)
		}
	}
	other := events[0]
	other.ID = "other-project"
	if _, err := memory.Record(context.Background(), "project-b", other); err != nil {
		t.Fatalf("record project-b history: %v", err)
	}

	first, err := memory.ListIssueEvents(
		context.Background(), "project-a", events[0].Fingerprint(), EventListOptions{Limit: 2},
	)
	if err != nil {
		t.Fatalf("list first history page: %v", err)
	}
	if first.Total != 3 || len(first.Events) != 2 || first.Next == nil {
		t.Fatalf("first history page = %#v, want two of three retained events", first)
	}
	if first.Events[0].ID != "event-04" || first.Events[1].ID != "event-03" {
		t.Fatalf("first history IDs = %q, %q", first.Events[0].ID, first.Events[1].ID)
	}
	second, err := memory.ListIssueEvents(
		context.Background(), "project-a", events[0].Fingerprint(),
		EventListOptions{Limit: 2, After: first.Next},
	)
	if err != nil {
		t.Fatalf("list second history page: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != "event-02" || second.Next != nil {
		t.Fatalf("second history page = %#v, want final retained event", second)
	}
	issue, err := memory.GetIssue(context.Background(), "project-a", events[0].Fingerprint())
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}
	if issue.Occurrences != 5 {
		t.Fatalf("aggregate occurrences = %d, want lifetime count 5", issue.Occurrences)
	}
	if _, err := memory.ListIssueEvents(
		context.Background(), "project-a", "missing", EventListOptions{},
	); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue history error = %v, want ErrIssueNotFound", err)
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

func TestMemoryRecordReopensResolvedIssueAndPreservesIgnoredIssue(t *testing.T) {
	memory := NewMemory()
	base := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)

	resolvedEvent := testEvent(base)
	resolvedEvent.Message = "resolved regression"
	resolved, err := memory.Record(context.Background(), "project-a", resolvedEvent)
	if err != nil {
		t.Fatalf("record resolved issue: %v", err)
	}
	if _, err := memory.SetIssueStatus(
		context.Background(), "project-a", resolved.Fingerprint, IssueStatusResolved,
	); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}
	reopened, err := memory.Record(
		context.Background(), "project-a", testEventWithMessage(base.Add(time.Minute), resolvedEvent.Message),
	)
	if err != nil {
		t.Fatalf("record regression: %v", err)
	}
	if reopened.Status != IssueStatusOpen || reopened.Occurrences != 2 {
		t.Fatalf("reopened issue = %#v, want open with two occurrences", reopened)
	}

	ignoredEvent := testEvent(base)
	ignoredEvent.Message = "ignored noise"
	ignored, err := memory.Record(context.Background(), "project-a", ignoredEvent)
	if err != nil {
		t.Fatalf("record ignored issue: %v", err)
	}
	if _, err := memory.SetIssueStatus(
		context.Background(), "project-a", ignored.Fingerprint, IssueStatusIgnored,
	); err != nil {
		t.Fatalf("ignore issue: %v", err)
	}
	stillIgnored, err := memory.Record(
		context.Background(), "project-a", testEventWithMessage(base.Add(time.Minute), ignoredEvent.Message),
	)
	if err != nil {
		t.Fatalf("record ignored recurrence: %v", err)
	}
	if stillIgnored.Status != IssueStatusIgnored || stillIgnored.Occurrences != 2 {
		t.Fatalf("ignored issue = %#v, want ignored with two occurrences", stillIgnored)
	}
}

func testEventWithMessage(receivedAt time.Time, message string) event.Event {
	captured := testEvent(receivedAt)
	captured.Message = message
	return captured
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

func historyEvents(base time.Time, count int, message string) []event.Event {
	events := make([]event.Event, count)
	for index := range count {
		events[index] = testEvent(base.Add(time.Duration(index) * time.Minute))
		events[index].ID = fmt.Sprintf("event-%02d", index)
		events[index].Message = message
	}
	return events
}
