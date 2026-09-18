package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
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

func TestSQLiteRecordMergesEveryV1IssueInTheRecurringV2Group(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 30, 18, 0, 0, 0, time.UTC)
	first := testEvent(base)
	first.ID = "legacy-query-one"
	first.Stack = "run@https://example.com/app.js?v=1:10:2"
	second := first
	second.ID = "legacy-query-two"
	second.Stack = "run@https://example.com/app.js?v=2:10:2"
	second.ReceivedAt = base.Add(time.Minute)
	otherFrame := first
	otherFrame.ID = "legacy-other-frame"
	otherFrame.Stack = "run@https://example.com/other.js?v=1:20:4"

	if first.Fingerprint() != second.Fingerprint() ||
		first.LegacyFingerprint() == second.LegacyFingerprint() {
		t.Fatal("test events must share v2 while retaining distinct v1 fingerprints")
	}
	if first.Fingerprint() == otherFrame.Fingerprint() {
		t.Fatal("the unrelated frame must remain in another v2 group")
	}
	type legacySeed struct {
		captured    event.Event
		status      IssueStatus
		occurrences int
		firstSeen   time.Time
		retained    int
	}
	seeds := []legacySeed{
		{first, IssueStatusIgnored, 7, base.Add(-time.Hour), 2},
		{second, IssueStatusOpen, 4, base.Add(-30 * time.Minute), 1},
		{otherFrame, IssueStatusOpen, 5, base.Add(-15 * time.Minute), 0},
	}
	for seedIndex, seed := range seeds {
		payload, err := json.Marshal(seed.captured)
		if err != nil {
			t.Fatalf("encode legacy issue %d: %v", seedIndex, err)
		}
		fingerprint := seed.captured.LegacyFingerprint()
		if _, err := database.db.Exec(`
INSERT INTO issues (
    project_id, fingerprint, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
			"project-a", fingerprint, seed.captured.Kind, seed.captured.Message,
			seed.captured.SourceURL, seed.captured.Line, seed.captured.Column,
			seed.status, seed.occurrences, seed.firstSeen.UnixNano(),
			seed.captured.ReceivedAt.UnixNano(), payload,
		); err != nil {
			t.Fatalf("seed legacy issue %d: %v", seedIndex, err)
		}
		for retainedIndex := range seed.retained {
			retained := seed.captured
			retained.ID = fmt.Sprintf("legacy-%d-%d", seedIndex, retainedIndex)
			retained.ReceivedAt = seed.captured.ReceivedAt.Add(
				-time.Duration(retainedIndex) * time.Second,
			)
			payload, err := json.Marshal(retained)
			if err != nil {
				t.Fatalf("encode retained legacy event %d/%d: %v", seedIndex, retainedIndex, err)
			}
			if _, err := database.db.Exec(`
INSERT INTO events (project_id, fingerprint, event_id, received_at, payload)
VALUES (?, ?, ?, ?, ?)
			`, "project-a", fingerprint, retained.ID, retained.ReceivedAt.UnixNano(), payload); err != nil {
				t.Fatalf("seed retained legacy event %d/%d: %v", seedIndex, retainedIndex, err)
			}
		}
	}

	recurrence := first
	recurrence.ID = "v2-recurrence"
	recurrence.Stack = "run@https://example.com/app.js?v=3:10:2"
	recurrence.ReceivedAt = base.Add(2 * time.Minute)
	issue, err := database.Record(context.Background(), "project-a", recurrence)
	if err != nil {
		t.Fatalf("record v2 recurrence: %v", err)
	}
	if issue.Fingerprint != recurrence.Fingerprint() {
		t.Fatalf("fingerprint = %q, want v2 %q", issue.Fingerprint, recurrence.Fingerprint())
	}
	if issue.Status != IssueStatusIgnored || issue.Occurrences != 12 {
		t.Fatalf("migrated issue = %#v, want ignored with 12 occurrences", issue)
	}
	if !issue.FirstSeen.Equal(base.Add(-time.Hour)) || issue.LastEvent.ID != recurrence.ID {
		t.Fatalf("migrated issue timestamps/event = %#v", issue)
	}
	history, err := database.ListIssueEvents(
		context.Background(), "project-a", issue.Fingerprint, EventListOptions{Limit: 10},
	)
	if err != nil {
		t.Fatalf("list migrated history: %v", err)
	}
	if history.Total != 4 || len(history.Events) != 4 {
		t.Fatalf("migrated history = %#v, want four retained events", history)
	}
	for _, migrated := range []event.Event{first, second} {
		if _, err := database.GetIssue(
			context.Background(), "project-a", migrated.LegacyFingerprint(),
		); !errors.Is(err, ErrIssueNotFound) {
			t.Fatalf("legacy issue lookup error = %v, want ErrIssueNotFound", err)
		}
	}
	if _, err := database.GetIssue(
		context.Background(), "project-a", otherFrame.LegacyFingerprint(),
	); err != nil {
		t.Fatalf("unrelated v1 issue was migrated: %v", err)
	}
	var legacyEvents int
	if err := database.db.QueryRow(
		"SELECT COUNT(*) FROM events WHERE project_id = ? AND fingerprint IN (?, ?)",
		"project-a", first.LegacyFingerprint(), second.LegacyFingerprint(),
	).Scan(&legacyEvents); err != nil {
		t.Fatalf("count legacy events: %v", err)
	}
	if legacyEvents != 0 {
		t.Fatalf("legacy event rows = %d, want 0 after migration", legacyEvents)
	}
}

func TestSQLiteRecordFindsANULLCollisionByItsExactV1Key(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, time.August, 30, 19, 0, 0, 0, time.UTC)
	first := testEvent(base)
	first.Message = "a\x00b"
	first.SourceURL = "c"
	later := first
	later.ID = "legacy-collision-latest"
	later.Message = "a"
	later.SourceURL = "b\x00c"
	if first.LegacyFingerprint() != later.LegacyFingerprint() ||
		first.Fingerprint() == later.Fingerprint() {
		t.Fatal("test events must collide in v1 and separate in v2")
	}
	payload, err := json.Marshal(later)
	if err != nil {
		t.Fatalf("encode colliding legacy event: %v", err)
	}
	if _, err := database.db.Exec(`
INSERT INTO issues (
    project_id, fingerprint, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		"project-a", first.LegacyFingerprint(), first.Kind, first.Message,
		first.SourceURL, first.Line, first.Column, IssueStatusIgnored, 2,
		base.Add(-time.Minute).UnixNano(), base.UnixNano(), payload,
	); err != nil {
		t.Fatalf("seed colliding v1 issue: %v", err)
	}

	recurrence := later
	recurrence.ID = "v2-collision-recurrence"
	recurrence.ReceivedAt = base.Add(time.Minute)
	issue, err := database.Record(context.Background(), "project-a", recurrence)
	if err != nil {
		t.Fatalf("record colliding recurrence: %v", err)
	}
	if issue.Fingerprint != recurrence.Fingerprint() ||
		issue.Message != recurrence.Message || issue.SourceURL != recurrence.SourceURL ||
		issue.Status != IssueStatusIgnored || issue.Occurrences != 3 {
		t.Fatalf("migrated collision issue = %#v", issue)
	}
	if _, err := database.GetIssue(
		context.Background(), "project-a", first.LegacyFingerprint(),
	); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("colliding v1 issue lookup error = %v, want ErrIssueNotFound", err)
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
	if version != 3 {
		t.Fatalf("new database schema version = %d, want 3", version)
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
	if version != 3 {
		t.Fatalf("migrated schema version = %d, want 3", version)
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
	var fingerprintVersion int
	if err := migrated.db.QueryRowContext(
		ctx, "SELECT fingerprint_version FROM issues WHERE project_id = 'project-a'",
	).Scan(&fingerprintVersion); err != nil {
		t.Fatalf("read adopted fingerprint version: %v", err)
	}
	if fingerprintVersion != 1 {
		t.Fatalf("adopted fingerprint version = %d, want 1", fingerprintVersion)
	}
	v2Event := testEvent(time.Now().UTC())
	v2Event.Message = "new v2 issue"
	if _, err := migrated.Record(ctx, "project-a", v2Event); err != nil {
		t.Fatalf("record v2 issue after migration: %v", err)
	}
	if err := migrated.db.QueryRowContext(
		ctx,
		"SELECT fingerprint_version FROM issues WHERE project_id = ? AND fingerprint = ?",
		"project-a", v2Event.Fingerprint(),
	).Scan(&fingerprintVersion); err != nil {
		t.Fatalf("read new fingerprint version: %v", err)
	}
	if fingerprintVersion != 2 {
		t.Fatalf("new fingerprint version = %d, want 2", fingerprintVersion)
	}
}

func TestSQLiteLegacyCandidateLookupUsesTheVersionedIndex(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	rows, err := database.db.QueryContext(
		context.Background(),
		"EXPLAIN QUERY PLAN "+sqliteLegacyIssueCandidates,
		"project-a", event.KindError, "boom", "https://example.com/app.js", 10, 2,
		"legacy-fingerprint",
	)
	if err != nil {
		t.Fatalf("explain legacy candidate lookup: %v", err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatalf("scan legacy candidate query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate legacy candidate query plan: %v", err)
	}
	plan := strings.ToLower(strings.Join(details, "\n"))
	if !strings.Contains(plan, "issues_legacy_fingerprint_lookup") {
		t.Fatalf("legacy candidate lookup does not use its versioned index:\n%s", plan)
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

func TestSQLiteMigrationsLockBeforeReadingTheSchemaVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migration-lock.db")
	dataSourceName := sqliteDataSourceName(path, 1)
	blocker, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		t.Fatalf("open migration blocker: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close() })
	blocker.SetMaxOpenConns(1)
	blocker.SetMaxIdleConns(1)
	blockerConnection, err := blocker.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire migration blocker: %v", err)
	}
	t.Cleanup(func() { _ = blockerConnection.Close() })
	if _, err := blockerConnection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_, _ = blockerConnection.ExecContext(context.Background(), "ROLLBACK")
		}
	})

	databases := make([]*sql.DB, 2)
	for index := range databases {
		databases[index], err = sql.Open("sqlite", dataSourceName)
		if err != nil {
			t.Fatalf("open migration database %d: %v", index, err)
		}
		databases[index].SetMaxOpenConns(1)
		databases[index].SetMaxIdleConns(1)
		t.Cleanup(func() { _ = databases[index].Close() })
	}
	migrations := []sqliteMigration{{
		version: 1,
		name:    "serialized migration",
		schema:  "CREATE TABLE migration_lock_test (id INTEGER)",
	}}
	errorsChannel := make(chan error, len(databases))
	for _, database := range databases {
		go func() {
			errorsChannel <- migrateSQLite(ctx, database, migrations)
		}()
	}
	for index, database := range databases {
		waitForSQLiteConnectionUse(t, database, fmt.Sprintf("migration database %d", index))
	}
	if _, err := blockerConnection.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatalf("release migration blocker: %v", err)
	}
	released = true
	for range databases {
		if err := <-errorsChannel; err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}

	var version int
	if err := databases[0].QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read serialized migration version: %v", err)
	}
	if version != 1 {
		t.Fatalf("serialized migration version = %d, want 1", version)
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

func TestSQLiteIssueCursorAddsAnIndexedLastSeenRange(t *testing.T) {
	database := openTestSQLite(t, ":memory:")
	t.Cleanup(func() { _ = database.Close() })
	lastSeen := time.Now().UTC().UnixNano()
	tests := []struct {
		name      string
		where     string
		arguments []any
		index     string
	}{
		{
			name:      "all statuses",
			where:     "project_id = ?" + sqliteIssueCursorPredicate,
			arguments: []any{"project-a", lastSeen, lastSeen, lastSeen, "fingerprint"},
			index:     "issues_project_last_seen_fingerprint",
		},
		{
			name:      "one status",
			where:     "project_id = ? AND status = ?" + sqliteIssueCursorPredicate,
			arguments: []any{"project-a", IssueStatusOpen, lastSeen, lastSeen, lastSeen, "fingerprint"},
			index:     "issues_project_status_last_seen_fingerprint",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments := append(append([]any{}, test.arguments...), 26, 0)
			rows, err := database.db.QueryContext(context.Background(), `
EXPLAIN QUERY PLAN
SELECT project_id, fingerprint
FROM issues
WHERE `+test.where+`
ORDER BY last_seen DESC, fingerprint ASC
LIMIT ? OFFSET ?
`, arguments...)
			if err != nil {
				t.Fatalf("explain cursor query: %v", err)
			}
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatalf("scan cursor query plan: %v", err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate cursor query plan: %v", err)
			}
			plan := strings.ToLower(strings.Join(details, "\n"))
			if !strings.Contains(plan, strings.ToLower(test.index)) ||
				!strings.Contains(plan, "last_seen<?") {
				t.Fatalf("cursor query plan does not use the composite range index:\n%s", plan)
			}
		})
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

func TestSQLiteOpenReconcilesALowerEventHistoryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-limit.db")
	database, err := OpenSQLiteWithOptions(
		context.Background(), path,
		SQLiteOptions{MaxOpenConnections: 1, MaxEventsPerIssue: 3},
	)
	if err != nil {
		t.Fatalf("open SQLite with initial history limit: %v", err)
	}
	groupCount := sqliteHistoryReconcileGroupBatch + 1
	captured := make([]event.Event, 0, groupCount*3)
	base := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)
	for group := range groupCount {
		for occurrence := range 3 {
			item := testEvent(base.Add(time.Duration(occurrence) * time.Minute))
			item.ID = fmt.Sprintf("history-%03d-%d", group, occurrence)
			item.Message = fmt.Sprintf("history group %03d", group)
			captured = append(captured, item)
		}
	}
	if _, err := database.RecordBatch(context.Background(), "project-a", captured); err != nil {
		t.Fatalf("seed event histories: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close SQLite with initial history limit: %v", err)
	}

	database, err = OpenSQLiteWithOptions(
		context.Background(), path,
		SQLiteOptions{MaxOpenConnections: 1, MaxEventsPerIssue: 2},
	)
	if err != nil {
		t.Fatalf("reopen SQLite with lower history limit: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var oversized, retained int
	if err := database.db.QueryRow(`
SELECT COUNT(*)
FROM (
    SELECT 1
    FROM events
    GROUP BY project_id, fingerprint
    HAVING COUNT(*) > 2
)
`).Scan(&oversized); err != nil {
		t.Fatalf("count oversized histories: %v", err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM events").Scan(&retained); err != nil {
		t.Fatalf("count retained event history: %v", err)
	}
	if oversized != 0 || retained != groupCount*2 {
		t.Fatalf(
			"oversized histories = %d, retained events = %d, want 0 and %d",
			oversized, retained, groupCount*2,
		)
	}
	var minimumOccurrences, maximumOccurrences int
	if err := database.db.QueryRow(
		"SELECT MIN(occurrences), MAX(occurrences) FROM issues",
	).Scan(&minimumOccurrences, &maximumOccurrences); err != nil {
		t.Fatalf("read lifetime occurrence counts: %v", err)
	}
	if minimumOccurrences != 3 || maximumOccurrences != 3 {
		t.Fatalf(
			"lifetime occurrences range = %d..%d, want 3..3",
			minimumOccurrences, maximumOccurrences,
		)
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

func TestSQLiteRecordSerializesConcurrentWALWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-writers.db")
	database, err := OpenSQLiteWithOptions(
		context.Background(), path, SQLiteOptions{MaxOpenConnections: 8},
	)
	if err != nil {
		t.Fatalf("open concurrent SQLite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	captured := testEvent(time.Now().UTC())
	const workers = 32

	start := make(chan struct{})
	errorsChannel := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, recordErr := database.Record(context.Background(), "project-a", captured)
			errorsChannel <- recordErr
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errorsChannel)
	for recordErr := range errorsChannel {
		if recordErr != nil {
			t.Fatalf("record event through WAL pool: %v", recordErr)
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

func TestSQLiteReadyRequiresTheOperationalSchema(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open schema probe database: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(sqliteInitialSchema); err != nil {
		t.Fatalf("create partial schema: %v", err)
	}
	store := &SQLite{db: database}
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("Ready() error = nil with the events table missing")
	}
	if _, err := database.Exec(sqliteEventHistorySchema); err != nil {
		t.Fatalf("complete operational schema: %v", err)
	}
	if err := store.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() with the complete schema: %v", err)
	}
	if _, err := database.Exec("DROP TABLE events"); err != nil {
		t.Fatalf("drop required events table: %v", err)
	}
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("Ready() error = nil after a required table was removed")
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

func waitForSQLiteConnectionUse(t *testing.T, database *sql.DB, description string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	consecutive := 0
	for {
		if database.Stats().InUse > 0 {
			consecutive++
			if consecutive >= 10 {
				return
			}
		} else {
			consecutive = 0
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s to reach the migration lock", description)
		case <-ticker.C:
		}
	}
}

func TestSQLiteReadOnlyDataSourceNameResolvesRelativePaths(t *testing.T) {
	directory := t.TempDir()
	chdir(t, directory)

	dataSourceName, err := sqliteReadOnlyDataSourceName("error-tracer.db")
	if err != nil {
		t.Fatalf("sqliteReadOnlyDataSourceName() error = %v", err)
	}
	// "file://error-tracer.db" makes SQLite read the first path segment as a
	// URI authority and reject the whole data source name.
	if !strings.HasPrefix(dataSourceName, "file:///") {
		t.Fatalf("dataSourceName = %q, want a file:/// URI", dataSourceName)
	}
	want := (&url.URL{Scheme: "file", Path: filepath.Join(directory, "error-tracer.db")}).String()
	if !strings.HasPrefix(dataSourceName, want+"?") {
		t.Fatalf("dataSourceName = %q, want prefix %q", dataSourceName, want)
	}

	explicit, err := sqliteReadOnlyDataSourceName("file:error-tracer.db")
	if err != nil {
		t.Fatalf("explicit sqliteReadOnlyDataSourceName() error = %v", err)
	}
	if !strings.HasPrefix(explicit, "file:error-tracer.db?") {
		t.Fatalf("explicit dataSourceName = %q, want an unchanged file: URI", explicit)
	}
	for _, parameter := range []string{"mode=ro", "_busy_timeout=5000", "_foreign_keys=on"} {
		if !strings.Contains(dataSourceName, parameter) {
			t.Fatalf("dataSourceName = %q, want it to contain %q", dataSourceName, parameter)
		}
	}
}

func TestSQLiteMaintenanceAcceptsRelativePaths(t *testing.T) {
	directory := t.TempDir()
	chdir(t, directory)

	database := openTestSQLite(t, filepath.Join(directory, "error-tracer.db"))
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	relative, err := OpenSQLiteReadOnly(context.Background(), "error-tracer.db")
	if err != nil {
		t.Fatalf("OpenSQLiteReadOnly(relative) error = %v", err)
	}
	if err := relative.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("relative IntegrityCheck() error = %v", err)
	}
	if err := relative.Close(); err != nil {
		t.Fatalf("relative Close() error = %v", err)
	}

	source := openTestSQLite(t, filepath.Join(directory, "error-tracer.db"))
	// The snapshot is staged next to the destination, so a relative
	// destination also has to survive the read-only reopen that verifies it.
	if err := source.Backup(context.Background(), "./backup.db"); err != nil {
		t.Fatalf("Backup(relative destination) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "backup.db")); err != nil {
		t.Fatalf("stat relative backup: %v", err)
	}
}

func chdir(t *testing.T, directory string) {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("Chdir(%q) error = %v", directory, err)
	}
	t.Cleanup(func() { _ = os.Chdir(working) })
}

func TestSQLiteEnforceIssueLimitEvictsOldestFirst(t *testing.T) {
	database := openTestSQLite(t, filepath.Join(t.TempDir(), "error-tracer.db"))
	base := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)

	// A fingerprint includes the message, so distinct messages are distinct
	// issues — which is exactly how a reporter drives up cardinality.
	for index := 0; index < 12; index++ {
		captured := event.Event{
			Kind:       event.KindError,
			Message:    fmt.Sprintf("failure-%02d", index),
			ReceivedAt: base.Add(time.Duration(index) * time.Minute),
		}
		captured.Normalize()
		if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("Record(%d) error = %v", index, err)
		}
	}

	evicted, err := database.EnforceIssueLimit(context.Background(), "project-a", 5)
	if err != nil {
		t.Fatalf("EnforceIssueLimit() error = %v", err)
	}
	if evicted != 7 {
		t.Fatalf("evicted = %d, want 7", evicted)
	}

	page, err := database.ListIssues(context.Background(), "project-a", ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if page.Total != 5 {
		t.Fatalf("remaining issues = %d, want 5", page.Total)
	}
	// The survivors must be the newest five, not an arbitrary five.
	for _, issue := range page.Issues {
		if issue.LastEvent.Message < "failure-07" {
			t.Fatalf("issue %q survived, want only the newest five", issue.LastEvent.Message)
		}
	}

	// Idempotent once the project is at or below the limit.
	again, err := database.EnforceIssueLimit(context.Background(), "project-a", 5)
	if err != nil {
		t.Fatalf("second EnforceIssueLimit() error = %v", err)
	}
	if again != 0 {
		t.Fatalf("second call evicted = %d, want 0", again)
	}
}

func TestSQLiteEnforceIssueLimitValidatesArguments(t *testing.T) {
	database := openTestSQLite(t, filepath.Join(t.TempDir(), "error-tracer.db"))

	if _, err := database.EnforceIssueLimit(context.Background(), " ", 10); !errors.Is(err, ErrProjectRequired) {
		t.Fatalf("empty project error = %v, want ErrProjectRequired", err)
	}
	if _, err := database.EnforceIssueLimit(context.Background(), "project-a", 0); !errors.Is(err, ErrIssueLimitRequired) {
		t.Fatalf("zero limit error = %v, want ErrIssueLimitRequired", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := database.EnforceIssueLimit(cancelled, "project-a", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v, want context.Canceled", err)
	}
}

func TestSQLiteEnforceIssueLimitCascadesEventHistory(t *testing.T) {
	database := openTestSQLite(t, filepath.Join(t.TempDir(), "error-tracer.db"))
	base := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)

	for index := 0; index < 3; index++ {
		captured := event.Event{
			Kind:       event.KindError,
			Message:    fmt.Sprintf("failure-%02d", index),
			ReceivedAt: base.Add(time.Duration(index) * time.Minute),
		}
		captured.Normalize()
		if _, err := database.Record(context.Background(), "project-a", captured); err != nil {
			t.Fatalf("Record(%d) error = %v", index, err)
		}
	}

	if _, err := database.EnforceIssueLimit(context.Background(), "project-a", 1); err != nil {
		t.Fatalf("EnforceIssueLimit() error = %v", err)
	}
	var events int
	if err := database.db.QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM events",
	).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Fatalf("retained events = %d, want 1 (the surviving issue's)", events)
	}
}

func TestOpenSQLiteWithOptionsHonoursContextCancellation(t *testing.T) {
	// Opening runs the migrations and the event-history reconcile. main used a
	// background context here, so a container receiving SIGTERM during a long
	// startup had to wait out the whole thing before it would stop.
	path := filepath.Join(t.TempDir(), "error-tracer.db")
	database := openTestSQLite(t, path)
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	opened, err := OpenSQLiteWithOptions(cancelled, path, SQLiteOptions{MaxOpenConnections: 1})
	if err == nil {
		_ = opened.Close()
		t.Fatal("OpenSQLiteWithOptions() error = nil for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenSQLiteWithOptions() error = %v, want context.Canceled", err)
	}
}

func TestSQLiteEnforceIssueLimitTakesTheOldestBatchFirst(t *testing.T) {
	// With more excess than PruneBatchSize, a single call must delete the
	// OLDEST batch. Ordering newest-first and skipping `limit` rows deletes
	// the newest of the excess instead, so an interrupted sweep would leave
	// the oldest issues behind — the opposite of the advertised policy. Both
	// orderings converge to the same final state, so only a partial sweep
	// distinguishes them.
	const (
		total = PruneBatchSize + 120
		limit = 50
	)
	database := openTestSQLite(t, filepath.Join(t.TempDir(), "error-tracer.db"))
	base := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)

	for start := 0; start < total; start += 100 {
		end := min(start+100, total)
		batch := make([]event.Event, 0, end-start)
		for index := start; index < end; index++ {
			captured := event.Event{
				Kind:       event.KindError,
				Message:    fmt.Sprintf("failure-%05d", index),
				ReceivedAt: base.Add(time.Duration(index) * time.Second),
			}
			captured.Normalize()
			batch = append(batch, captured)
		}
		if _, err := database.RecordBatch(context.Background(), "project-a", batch); err != nil {
			t.Fatalf("RecordBatch(%d) error = %v", start, err)
		}
	}

	evicted, err := database.EnforceIssueLimit(context.Background(), "project-a", limit)
	if err != nil {
		t.Fatalf("EnforceIssueLimit() error = %v", err)
	}
	if evicted != PruneBatchSize {
		t.Fatalf("evicted = %d, want %d", evicted, PruneBatchSize)
	}

	page, err := database.ListIssues(context.Background(), "project-a", ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if page.Total != total-PruneBatchSize {
		t.Fatalf("remaining = %d, want %d", page.Total, total-PruneBatchSize)
	}
	// Survivors must be one contiguous newest block. The oldest one left is
	// index PruneBatchSize; anything older means a newer issue was deleted
	// ahead of it.
	oldestKept := fmt.Sprintf("failure-%05d", PruneBatchSize)
	for _, issue := range page.Issues {
		if issue.LastEvent.Message < oldestKept {
			t.Fatalf(
				"issue %q survived while newer issues were evicted; oldest allowed is %q",
				issue.LastEvent.Message, oldestKept,
			)
		}
	}
}
