package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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

func TestSQLiteMigrationsVersionNewAndExistingDatabases(t *testing.T) {
	ctx := context.Background()
	newPath := filepath.Join(t.TempDir(), "new.db")
	created := openTestSQLite(t, newPath)
	var version int
	if err := created.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read new database schema version: %v", err)
	}
	if version != 2 {
		t.Fatalf("new database schema version = %d, want 2", version)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close new database: %v", err)
	}

	existingPath := filepath.Join(t.TempDir(), "existing.db")
	existing, err := sql.Open("sqlite", existingPath)
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	if _, err := existing.Exec(sqliteInitialSchema); err != nil {
		t.Fatalf("create unversioned schema: %v", err)
	}
	if _, err := existing.Exec(`
		INSERT INTO issues (
			project_id, fingerprint, kind, message, source_url, line, column_number,
			status, occurrences, first_seen, last_seen, last_event
		) VALUES ('project-a', 'fingerprint', 'error', 'preserved', '', 0, 0,
			'open', 1, 1, 1, '{}')
	`); err != nil {
		t.Fatalf("seed unversioned database: %v", err)
	}
	if err := existing.Close(); err != nil {
		t.Fatalf("close unversioned database: %v", err)
	}

	migrated := openTestSQLite(t, existingPath)
	t.Cleanup(func() { _ = migrated.Close() })
	if err := migrated.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if version != 2 {
		t.Fatalf("migrated schema version = %d, want 2", version)
	}
	var message string
	if err := migrated.db.QueryRowContext(
		ctx, "SELECT message FROM issues WHERE project_id = 'project-a'",
	).Scan(&message); err != nil {
		t.Fatalf("read preserved row: %v", err)
	}
	if message != "preserved" {
		t.Fatalf("preserved message = %q, want preserved", message)
	}
}

func TestSQLiteMigrationRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := database.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set newer schema version: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if _, err := OpenSQLite(context.Background(), path); !errors.Is(err, ErrSQLiteSchemaTooNew) {
		t.Fatalf("open newer schema error = %v, want ErrSQLiteSchemaTooNew", err)
	}
}

func TestSQLiteMigrationRollsBackFailedVersion(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations := []sqliteMigration{{
		version: 1,
		name:    "broken migration",
		schema:  "CREATE TABLE partial (id INTEGER); INVALID SQL;",
	}}
	if err := migrateSQLite(context.Background(), database, migrations); err == nil {
		t.Fatal("failed migration error = nil")
	}
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 0 {
		t.Fatalf("schema version = %d, want 0", version)
	}
	var count int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'partial'",
	).Scan(&count); err != nil {
		t.Fatalf("inspect partial table: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial table count = %d, want 0", count)
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

	if _, err := database.db.Exec(`
	CREATE TRIGGER reject_event_history
	BEFORE INSERT ON events
	WHEN NEW.event_id = 'reject-history'
	BEGIN
	    SELECT RAISE(ABORT, 'rejected event history');
	END;
	`); err != nil {
		t.Fatalf("create event-history rejection trigger: %v", err)
	}
	historyRejected := testEvent(base.Add(4 * time.Minute))
	historyRejected.ID = "reject-history"
	historyRejected.Message = "history transaction rollback"
	if _, err := database.Record(
		context.Background(), "project-a", historyRejected,
	); err == nil {
		t.Fatal("record rejected history error = nil")
	}
	if _, err := database.GetIssue(
		context.Background(), "project-a", historyRejected.Fingerprint(),
	); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("issue from failed history insert error = %v, want ErrIssueNotFound", err)
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

func TestSQLiteListIssuesCursorHandlesEqualTimestamps(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	receivedAt := time.Date(2026, time.August, 29, 1, 0, 0, 0, time.UTC)
	for _, message := range []string{"cursor-a", "cursor-b", "cursor-c"} {
		captured := testEvent(receivedAt)
		captured.Message = message
		if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record %q: %v", message, err)
		}
	}

	first, err := database.ListIssues(context.Background(), "project-a", ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Issues) != 2 || first.Next == nil {
		t.Fatalf("first page = %#v, want two issues and a cursor", first)
	}
	second, err := database.ListIssues(
		context.Background(), "project-a", ListOptions{Limit: 2, After: first.Next},
	)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.Issues) != 1 || second.Next != nil {
		t.Fatalf("second page = %#v, want final issue without cursor", second)
	}
	fingerprints := []string{
		first.Issues[0].Fingerprint,
		first.Issues[1].Fingerprint,
		second.Issues[0].Fingerprint,
	}
	if !sort.StringsAreSorted(fingerprints) {
		t.Fatalf("fingerprints = %#v, want stable ascending tie-break order", fingerprints)
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

func TestSQLiteListIssueEventsIsBoundedProjectScopedAndCursorPaginated(t *testing.T) {
	database, err := OpenSQLiteWithOptions(
		context.Background(), ":memory:",
		SQLiteOptions{MaxOpenConnections: 1, MaxEventsPerIssue: 3},
	)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)
	events := historyEvents(base, 5, "bounded history")
	events[3].ReceivedAt = events[4].ReceivedAt
	for _, captured := range events {
		if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("record project-a history: %v", err)
		}
	}
	other := events[0]
	other.ID = "other-project"
	if _, err := database.Record(context.Background(), "project-b", other); err != nil {
		t.Fatalf("record project-b history: %v", err)
	}

	first, err := database.ListIssueEvents(
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
	second, err := database.ListIssueEvents(
		context.Background(), "project-a", events[0].Fingerprint(),
		EventListOptions{Limit: 2, After: first.Next},
	)
	if err != nil {
		t.Fatalf("list second history page: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != "event-02" || second.Next != nil {
		t.Fatalf("second history page = %#v, want final retained event", second)
	}
	issue, err := database.GetIssue(context.Background(), "project-a", events[0].Fingerprint())
	if err != nil {
		t.Fatalf("get aggregate: %v", err)
	}
	if issue.Occurrences != 5 {
		t.Fatalf("aggregate occurrences = %d, want lifetime count 5", issue.Occurrences)
	}
	if _, err := database.ListIssueEvents(
		context.Background(), "project-a", "missing", EventListOptions{},
	); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue history error = %v, want ErrIssueNotFound", err)
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

func TestSQLiteConcurrentPoolUsesWALAndConnectionPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	database, err := OpenSQLiteWithOptions(
		context.Background(), path, SQLiteOptions{MaxOpenConnections: 4},
	)
	if err != nil {
		t.Fatalf("open concurrent SQLite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if got := database.db.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("MaxOpenConnections = %d, want 4", got)
	}

	connections := make([]*sql.Conn, 0, 4)
	for range 4 {
		connection, err := database.db.Conn(context.Background())
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		connections = append(connections, connection)
		var journalMode string
		var foreignKeys, busyTimeout int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("read journal mode: %v", err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("read foreign keys: %v", err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("read busy timeout: %v", err)
		}
		if !strings.EqualFold(journalMode, "wal") || foreignKeys != 1 || busyTimeout != 5000 {
			t.Fatalf("connection pragmas = journal:%q foreign_keys:%d busy_timeout:%d", journalMode, foreignKeys, busyTimeout)
		}
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatalf("close connection: %v", err)
		}
	}

	captured := testEvent(time.Now().UTC())
	if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	writer, err := database.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive transaction: %v", err)
	}
	defer func() { _, _ = writer.ExecContext(context.Background(), "ROLLBACK") }()
	if _, err := writer.ExecContext(
		context.Background(),
		"UPDATE issues SET status = 'ignored' WHERE project_id = 'project-a'",
	); err != nil {
		t.Fatalf("hold write transaction: %v", err)
	}

	readContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	page, err := database.ListIssues(readContext, "project-a", ListOptions{})
	if err != nil {
		t.Fatalf("read while writer is active: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].Status != IssueStatusOpen {
		t.Fatalf("reader snapshot = %#v, want pre-transaction issue", page.Issues)
	}
}

func TestSQLiteRejectsInvalidInputAndMapsMissingIssue(t *testing.T) {
	if _, err := OpenSQLite(context.Background(), "  "); !errors.Is(err, ErrDatabasePathRequired) {
		t.Fatalf("empty path error = %v, want ErrDatabasePathRequired", err)
	}
	if _, err := OpenSQLiteWithOptions(
		context.Background(), ":memory:", SQLiteOptions{MaxOpenConnections: 2},
	); !errors.Is(err, ErrConcurrentMemoryDatabase) {
		t.Fatalf("concurrent memory error = %v, want ErrConcurrentMemoryDatabase", err)
	}
	if _, err := OpenSQLiteWithOptions(
		context.Background(), "ignored.db", SQLiteOptions{MaxOpenConnections: 33},
	); !errors.Is(err, ErrInvalidSQLiteConnectionCount) {
		t.Fatalf("connection count error = %v, want ErrInvalidSQLiteConnectionCount", err)
	}
	if _, err := OpenSQLiteWithOptions(
		context.Background(), "ignored.db",
		SQLiteOptions{MaxOpenConnections: 1, MaxEventsPerIssue: MaxEventsPerIssue + 1},
	); !errors.Is(err, ErrInvalidEventHistoryLimit) {
		t.Fatalf("event history limit error = %v, want ErrInvalidEventHistoryLimit", err)
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

func TestSQLiteRecordReopensResolvedIssueAndPreservesIgnoredIssue(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)

	resolvedEvent := testEvent(base)
	resolvedEvent.Message = "resolved regression"
	resolved, err := database.Record(context.Background(), "project-a", resolvedEvent)
	if err != nil {
		t.Fatalf("record resolved issue: %v", err)
	}
	if _, err := database.SetIssueStatus(
		context.Background(), "project-a", resolved.Fingerprint, IssueStatusResolved,
	); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}
	reopened, err := database.Record(
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
	ignored, err := database.Record(context.Background(), "project-a", ignoredEvent)
	if err != nil {
		t.Fatalf("record ignored issue: %v", err)
	}
	if _, err := database.SetIssueStatus(
		context.Background(), "project-a", ignored.Fingerprint, IssueStatusIgnored,
	); err != nil {
		t.Fatalf("ignore issue: %v", err)
	}
	stillIgnored, err := database.Record(
		context.Background(), "project-a", testEventWithMessage(base.Add(time.Minute), ignoredEvent.Message),
	)
	if err != nil {
		t.Fatalf("record ignored recurrence: %v", err)
	}
	if stillIgnored.Status != IssueStatusIgnored || stillIgnored.Occurrences != 2 {
		t.Fatalf("ignored issue = %#v, want ignored with two occurrences", stillIgnored)
	}
}

func TestSQLitePruneIssuesIsProjectScopedAndUsesLastSeen(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	cutoff := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	old := testEvent(cutoff.Add(-time.Hour))
	old.Message = "expired"
	current := testEvent(cutoff)
	current.Message = "at cutoff"
	otherProject := testEvent(cutoff.Add(-time.Hour))
	otherProject.Message = "other project"
	for _, item := range []event.Event{old, current} {
		if _, err := database.Record(context.Background(), "project-a", item); err != nil {
			t.Fatalf("record project-a issue: %v", err)
		}
	}
	if _, err := database.Record(context.Background(), "project-b", otherProject); err != nil {
		t.Fatalf("record project-b issue: %v", err)
	}

	deleted, err := database.PruneIssues(context.Background(), "project-a", cutoff)
	if err != nil {
		t.Fatalf("PruneIssues() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PruneIssues() deleted = %d, want 1", deleted)
	}
	if _, err := database.GetIssue(context.Background(), "project-a", old.Fingerprint()); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("expired issue error = %v, want ErrIssueNotFound", err)
	}
	var oldEvents int
	if err := database.db.QueryRow(
		"SELECT COUNT(*) FROM events WHERE project_id = ? AND fingerprint = ?",
		"project-a", old.Fingerprint(),
	).Scan(&oldEvents); err != nil {
		t.Fatalf("count expired event history: %v", err)
	}
	if oldEvents != 0 {
		t.Fatalf("expired event history rows = %d, want 0", oldEvents)
	}
	if _, err := database.GetIssue(context.Background(), "project-a", current.Fingerprint()); err != nil {
		t.Fatalf("issue at cutoff was deleted: %v", err)
	}
	if _, err := database.GetIssue(context.Background(), "project-b", otherProject.Fingerprint()); err != nil {
		t.Fatalf("other project issue was deleted: %v", err)
	}

	if _, err := database.PruneIssues(context.Background(), "", cutoff); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("empty project error = %v, want ErrProjectRequired", err)
	}
	if _, err := database.PruneIssues(context.Background(), "project-a", time.Time{}); !errors.Is(err, ErrCutoffRequired) {
		t.Fatalf("empty cutoff error = %v, want ErrCutoffRequired", err)
	}
}

func TestSQLitePruneIssuesBoundsEachDelete(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	captured := make([]event.Event, PruneBatchSize+3)
	for index := range captured {
		captured[index] = testEvent(time.Date(2026, time.January, 1, 0, 0, index, 0, time.UTC))
		captured[index].Message = fmt.Sprintf("expired-%d", index)
	}
	if _, err := database.RecordBatch(context.Background(), "project-a", captured); err != nil {
		t.Fatalf("record expired issues: %v", err)
	}

	cutoff := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	deleted, err := database.PruneIssues(context.Background(), "project-a", cutoff)
	if err != nil {
		t.Fatalf("first PruneIssues() error = %v", err)
	}
	if deleted != PruneBatchSize {
		t.Fatalf("first PruneIssues() deleted = %d, want %d", deleted, PruneBatchSize)
	}
	deleted, err = database.PruneIssues(context.Background(), "project-a", cutoff)
	if err != nil {
		t.Fatalf("second PruneIssues() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("second PruneIssues() deleted = %d, want 3", deleted)
	}
}

func TestSQLiteReadOnlyMaintenanceAndAtomicBackup(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source #1.db")
	backupPath := filepath.Join(directory, "backup.db")
	database, err := OpenSQLiteWithOptions(
		context.Background(), sourcePath, SQLiteOptions{MaxOpenConnections: 4},
	)
	if err != nil {
		t.Fatalf("open source SQLite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	stored, err := database.Record(context.Background(), "project-a", testEvent(time.Now().UTC()))
	if err != nil {
		t.Fatalf("record source issue: %v", err)
	}
	if err := database.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}
	if err := database.Backup(context.Background(), backupPath); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if err := database.Backup(context.Background(), backupPath); !errors.Is(err, ErrBackupExists) {
		t.Fatalf("existing Backup() error = %v, want ErrBackupExists", err)
	}
	if matches, err := filepath.Glob(filepath.Join(directory, ".backup.db-*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary backups = %v, error = %v", matches, err)
	}

	backup, err := OpenSQLiteReadOnly(context.Background(), backupPath)
	if err != nil {
		t.Fatalf("open backup read-only: %v", err)
	}
	t.Cleanup(func() { _ = backup.Close() })
	if err := backup.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("backup IntegrityCheck() error = %v", err)
	}
	if _, err := backup.GetIssue(context.Background(), "project-a", stored.Fingerprint); err != nil {
		t.Fatalf("read issue from backup: %v", err)
	}
	if _, err := OpenSQLiteReadOnly(context.Background(), filepath.Join(directory, "missing.db")); err == nil {
		t.Fatal("OpenSQLiteReadOnly() error = nil for a missing database")
	}
	if err := database.Backup(context.Background(), " "); !errors.Is(err, ErrBackupPathRequired) {
		t.Fatalf("empty Backup() error = %v, want ErrBackupPathRequired", err)
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
