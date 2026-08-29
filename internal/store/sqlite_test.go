package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
)

func TestSQLiteRecordPersistsAggregatedIssue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error-tracer.db")
	firstTime := time.Date(2026, time.August, 29, 1, 0, 0, 123, time.UTC)
	secondTime := firstTime.Add(time.Minute)

	database := openTestSQLite(t, path)
	first := testEvent(firstTime)
	second := testEvent(secondTime)
	second.Release = "2.0.0"
	if _, err := database.Record(context.Background(), "project-a", first); err != nil {
		t.Fatalf("record first event: %v", err)
	}
	issue, err := database.Record(context.Background(), "project-a", second)
	if err != nil {
		t.Fatalf("record second event: %v", err)
	}
	if issue.Occurrences != 2 || !issue.FirstSeen.Equal(firstTime) || !issue.LastSeen.Equal(secondTime) {
		t.Fatalf("aggregated issue = %#v", issue)
	}
	if issue.LastEvent.Release != "2.0.0" {
		t.Fatalf("last event release = %q, want latest event", issue.LastEvent.Release)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened := openTestSQLite(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.GetIssue(context.Background(), "project-a", first.Fingerprint())
	if err != nil {
		t.Fatalf("get persisted issue: %v", err)
	}
	if persisted.Occurrences != 2 || persisted.LastEvent.Release != "2.0.0" {
		t.Fatalf("persisted issue = %#v", persisted)
	}
}

func TestSQLiteRecordBatchUsesOneAtomicTransaction(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	first := testEvent(base)
	second := testEvent(base.Add(time.Minute))
	second.Release = "2.0.0"

	issues, err := database.RecordBatch(
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

	if _, err := database.db.Exec(`
CREATE TRIGGER reject_batch_event
BEFORE INSERT ON issues
WHEN NEW.message = 'reject batch'
BEGIN
    SELECT RAISE(ABORT, 'rejected batch event');
END;
`); err != nil {
		t.Fatalf("create rejection trigger: %v", err)
	}
	accepted := testEvent(base.Add(2 * time.Minute))
	accepted.Message = "would be written without rollback"
	rejected := testEvent(base.Add(3 * time.Minute))
	rejected.Message = "reject batch"
	if _, err := database.RecordBatch(
		context.Background(), "project-a", []event.Event{accepted, rejected},
	); err == nil {
		t.Fatal("record rejected batch error = nil")
	}
	if _, err := database.GetIssue(
		context.Background(), "project-a", accepted.Fingerprint(),
	); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("first event in failed batch error = %v, want ErrIssueNotFound", err)
	}
	if _, err := database.RecordBatch(
		context.Background(), "project-a", nil,
	); !errors.Is(err, ErrEventsRequired) {
		t.Fatalf("empty batch error = %v, want ErrEventsRequired", err)
	}
}

func TestSQLiteKeepsOlderEventFromReplacingLastEvent(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	newer := testEvent(time.Date(2026, time.August, 29, 2, 0, 0, 0, time.UTC))
	newer.Release = "newer"
	older := testEvent(newer.ReceivedAt.Add(-time.Hour))
	older.Release = "older"

	if _, err := database.Record(context.Background(), "project-a", newer); err != nil {
		t.Fatalf("record newer event: %v", err)
	}
	issue, err := database.Record(context.Background(), "project-a", older)
	if err != nil {
		t.Fatalf("record older event: %v", err)
	}
	if issue.Occurrences != 2 || issue.LastEvent.Release != "newer" {
		t.Fatalf("issue = %#v, want newer last event and two occurrences", issue)
	}
	if !issue.FirstSeen.Equal(older.ReceivedAt) {
		t.Fatalf("first seen = %s, want %s", issue.FirstSeen, older.ReceivedAt)
	}
}

func TestSQLiteListIssuesIsProjectScopedBoundedAndOrdered(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)

	for index := 0; index < 3; index++ {
		captured := testEvent(base.Add(time.Duration(index) * time.Minute))
		captured.Message = fmt.Sprintf("failure-%d", index)
		if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record project-a event %d: %v", index, err)
		}
	}
	if _, err := database.Record(context.Background(), "project-b", testEvent(base)); err != nil {
		t.Fatalf("record project-b event: %v", err)
	}

	page, err := database.ListIssues(context.Background(), "project-a", ListOptions{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if page.Total != 3 || len(page.Issues) != 1 || page.Issues[0].Message != "failure-1" {
		t.Fatalf("page = %#v, want second-most-recent of three project-a issues", page)
	}
	if page.Issues[0].ProjectID != "project-a" {
		t.Fatalf("project ID = %q, want project-a", page.Issues[0].ProjectID)
	}
}

func TestSQLiteListIssuesFiltersByStatus(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	openEvent := testEvent(base)
	openEvent.Message = "open failure"
	if _, err := database.Record(context.Background(), "project-a", openEvent); err != nil {
		t.Fatalf("record open issue: %v", err)
	}
	ignoredEvent := testEvent(base.Add(time.Minute))
	ignoredEvent.Message = "ignored failure"
	ignored, err := database.Record(context.Background(), "project-a", ignoredEvent)
	if err != nil {
		t.Fatalf("record ignored issue: %v", err)
	}
	if _, err := database.SetIssueStatus(
		context.Background(), "project-a", ignored.Fingerprint, IssueStatusIgnored,
	); err != nil {
		t.Fatalf("ignore issue: %v", err)
	}

	page, err := database.ListIssues(
		context.Background(), "project-a", ListOptions{Status: IssueStatusIgnored},
	)
	if err != nil {
		t.Fatalf("list ignored issues: %v", err)
	}
	if page.Total != 1 || len(page.Issues) != 1 ||
		page.Issues[0].Message != "ignored failure" {
		t.Fatalf("ignored page = %#v, want only the ignored issue", page)
	}
	if _, err := database.ListIssues(
		context.Background(), "project-a", ListOptions{Status: "closed"},
	); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidStatus", err)
	}
}

func TestSQLiteRecordSerializesConcurrentWriters(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	captured := testEvent(time.Now().UTC())
	const workers = 50

	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, workers)
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := database.Record(context.Background(), "project-a", captured)
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

	issue, err := database.GetIssue(context.Background(), "project-a", captured.Fingerprint())
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.Occurrences != workers {
		t.Fatalf("occurrences = %d, want %d", issue.Occurrences, workers)
	}
}

func TestSQLiteRejectsInvalidInputAndMapsMissingIssue(t *testing.T) {
	if _, err := OpenSQLite(context.Background(), "  "); !errors.Is(err, ErrDatabasePathRequired) {
		t.Fatalf("empty path error = %v, want ErrDatabasePathRequired", err)
	}

	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	captured := testEvent(time.Now().UTC())
	if _, err := database.Record(context.Background(), "", captured); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("empty project error = %v, want ErrProjectRequired", err)
	}
	captured.ReceivedAt = time.Time{}
	if _, err := database.Record(context.Background(), "project-a", captured); !errors.Is(err, ErrReceivedAtEmpty) {
		t.Fatalf("empty received_at error = %v, want ErrReceivedAtEmpty", err)
	}
	if _, err := database.GetIssue(context.Background(), "project-a", "missing"); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue error = %v, want ErrIssueNotFound", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := database.ListIssues(ctx, "project-a", ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v, want context.Canceled", err)
	}
}

func TestSQLiteSetIssueStatusPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error-tracer.db")
	database := openTestSQLite(t, path)
	captured := testEvent(time.Now().UTC())
	stored, err := database.Record(context.Background(), "project-a", captured)
	if err != nil {
		t.Fatalf("record issue: %v", err)
	}
	updated, err := database.SetIssueStatus(
		context.Background(), "project-a", stored.Fingerprint, IssueStatusIgnored,
	)
	if err != nil {
		t.Fatalf("set issue status: %v", err)
	}
	if updated.Status != IssueStatusIgnored {
		t.Fatalf("status = %q, want %q", updated.Status, IssueStatusIgnored)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened := openTestSQLite(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.GetIssue(context.Background(), "project-a", stored.Fingerprint)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if persisted.Status != IssueStatusIgnored {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, IssueStatusIgnored)
	}
	if _, err := reopened.SetIssueStatus(context.Background(), "project-a", stored.Fingerprint, "closed"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidStatus", err)
	}
	if _, err := reopened.SetIssueStatus(context.Background(), "project-a", "missing", IssueStatusOpen); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue error = %v, want ErrIssueNotFound", err)
	}
}

func openTestSQLite(t *testing.T, path string) *SQLite {
	t.Helper()
	database, err := OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	return database
}
