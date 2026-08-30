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
	"strings"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	_ "modernc.org/sqlite"
)

const sqliteInitialSchema = `
CREATE TABLE IF NOT EXISTS issues (
    project_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    kind TEXT NOT NULL,
    message TEXT NOT NULL,
    source_url TEXT NOT NULL,
    line INTEGER NOT NULL,
    column_number INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('open', 'resolved', 'ignored')),
    occurrences INTEGER NOT NULL CHECK (occurrences > 0),
    first_seen INTEGER NOT NULL,
    last_seen INTEGER NOT NULL,
    last_event BLOB NOT NULL,
    PRIMARY KEY (project_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS issues_project_status_last_seen
    ON issues (project_id, status, last_seen DESC);

CREATE INDEX IF NOT EXISTS issues_project_last_seen
    ON issues (project_id, last_seen);

CREATE INDEX IF NOT EXISTS issues_project_last_seen_fingerprint
    ON issues (project_id, last_seen DESC, fingerprint ASC);

CREATE INDEX IF NOT EXISTS issues_project_status_last_seen_fingerprint
    ON issues (project_id, status, last_seen DESC, fingerprint ASC);
`

const sqliteEventHistorySchema = `
CREATE TABLE events (
    sequence INTEGER PRIMARY KEY,
    project_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    event_id TEXT NOT NULL,
    received_at INTEGER NOT NULL,
    payload BLOB NOT NULL,
    FOREIGN KEY (project_id, fingerprint)
        REFERENCES issues (project_id, fingerprint) ON DELETE CASCADE
);

CREATE INDEX events_issue_received_sequence
    ON events (project_id, fingerprint, received_at DESC, sequence DESC);
`

type sqliteMigration struct {
	version int
	name    string
	schema  string
}

var sqliteMigrations = []sqliteMigration{
	{version: 1, name: "create issues", schema: sqliteInitialSchema},
	{version: 2, name: "create event history", schema: sqliteEventHistorySchema},
}

var ErrSQLiteSchemaTooNew = errors.New("SQLite schema is newer than this Error-Tracer build")

const sqliteUpsertIssue = `
INSERT INTO issues (
    project_id, fingerprint, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT (project_id, fingerprint) DO UPDATE SET
    occurrences = issues.occurrences + 1,
    first_seen = MIN(issues.first_seen, excluded.first_seen),
    status = CASE
        WHEN issues.status = 'resolved' THEN excluded.status
        ELSE issues.status
    END,
    last_event = CASE
        WHEN excluded.last_seen >= issues.last_seen THEN excluded.last_event
        ELSE issues.last_event
    END,
    last_seen = MAX(issues.last_seen, excluded.last_seen)
`

const sqliteInsertEvent = `
INSERT INTO events (project_id, fingerprint, event_id, received_at, payload)
VALUES (?, ?, ?, ?, ?)
`

const sqliteMergeLegacyIssue = `
INSERT INTO issues (
    project_id, fingerprint, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
)
SELECT
    project_id, ?, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
FROM issues
WHERE project_id = ? AND fingerprint = ?
ON CONFLICT (project_id, fingerprint) DO UPDATE SET
    status = CASE
        WHEN issues.status = 'ignored' OR excluded.status = 'ignored' THEN 'ignored'
        WHEN issues.status = 'open' OR excluded.status = 'open' THEN 'open'
        ELSE 'resolved'
    END,
    occurrences = issues.occurrences + excluded.occurrences,
    first_seen = MIN(issues.first_seen, excluded.first_seen),
    last_event = CASE
        WHEN excluded.last_seen >= issues.last_seen THEN excluded.last_event
        ELSE issues.last_event
    END,
    last_seen = MAX(issues.last_seen, excluded.last_seen)
`

const sqliteTrimEvents = `
DELETE FROM events
WHERE sequence IN (
    SELECT sequence
    FROM events
    WHERE project_id = ? AND fingerprint = ?
    ORDER BY received_at DESC, sequence DESC
    LIMIT -1 OFFSET ?
)
`

const (
	sqliteHistoryReconcileGroupBatch = 100
	sqliteIssueCursorPredicate       = " AND last_seen <= ? AND (last_seen < ? OR (last_seen = ? AND fingerprint > ?))"
)

var (
	ErrDatabasePathRequired = errors.New("database path is required")
	ErrBackupPathRequired   = errors.New("backup path is required")
	ErrBackupExists         = errors.New("backup path already exists")
	ErrIntegrityCheckFailed = errors.New("SQLite integrity check failed")
)

var (
	ErrInvalidSQLiteConnectionCount = errors.New("SQLite connection count must be between 1 and 32")
	ErrConcurrentMemoryDatabase     = errors.New("in-memory SQLite databases require one connection")
)

// SQLiteOptions controls the database/sql pool used by SQLite. More than one
// connection enables WAL so reads can continue while the single SQLite writer
// is committing.
type SQLiteOptions struct {
	MaxOpenConnections int
	MaxEventsPerIssue  int
}

// SQLite persists aggregated issues in a local SQLite database.
type SQLite struct {
	db                *sql.DB
	maxEventsPerIssue int
}

// OpenSQLite opens a SQLite database and applies its idempotent schema.
func OpenSQLite(ctx context.Context, path string) (*SQLite, error) {
	return OpenSQLiteWithOptions(ctx, path, SQLiteOptions{MaxOpenConnections: 1})
}

// OpenSQLiteReadOnly opens an existing database without applying migrations or
// changing its journal mode. It is intended for integrity checks and backups.
func OpenSQLiteReadOnly(ctx context.Context, path string) (*SQLite, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrDatabasePathRequired
	}
	database, err := sql.Open("sqlite", sqliteReadOnlyDataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("open read-only sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping read-only sqlite database: %w", err)
	}
	return &SQLite{db: database}, nil
}

// OpenSQLiteWithOptions opens a SQLite database with an explicitly bounded
// connection pool and applies its idempotent schema.
func OpenSQLiteWithOptions(ctx context.Context, path string, options SQLiteOptions) (*SQLite, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrDatabasePathRequired
	}
	connections := options.MaxOpenConnections
	if connections == 0 {
		connections = 1
	}
	if connections < 1 || connections > 32 {
		return nil, ErrInvalidSQLiteConnectionCount
	}
	if connections > 1 && isMemorySQLitePath(path) {
		return nil, ErrConcurrentMemoryDatabase
	}
	maxEvents := options.MaxEventsPerIssue
	if maxEvents == 0 {
		maxEvents = DefaultMaxEventsPerIssue
	}
	if maxEvents < 1 || maxEvents > MaxEventsPerIssue {
		return nil, ErrInvalidEventHistoryLimit
	}

	database, err := sql.Open("sqlite", sqliteDataSourceName(path, connections))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(connections)
	database.SetMaxIdleConns(connections)

	fail := func(operation string, operationErr error) (*SQLite, error) {
		_ = database.Close()
		return nil, fmt.Errorf("%s: %w", operation, operationErr)
	}

	if err := database.PingContext(ctx); err != nil {
		return fail("ping sqlite database", err)
	}
	if connections > 1 {
		var journalMode string
		if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			return fail("read sqlite journal mode", err)
		}
		if !strings.EqualFold(journalMode, "wal") {
			return fail("configure sqlite database", fmt.Errorf("journal mode is %q, want WAL", journalMode))
		}
	}
	if err := migrateSQLite(ctx, database, sqliteMigrations); err != nil {
		return fail("migrate sqlite database", err)
	}
	if err := reconcileSQLiteEventHistory(ctx, database, maxEvents); err != nil {
		return fail("reconcile sqlite event history", err)
	}

	return &SQLite{db: database, maxEventsPerIssue: maxEvents}, nil
}

func migrateSQLite(ctx context.Context, database *sql.DB, migrations []sqliteMigration) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin immediate migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var currentVersion int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&currentVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	latestVersion := 0
	if len(migrations) > 0 {
		latestVersion = migrations[len(migrations)-1].version
	}
	if currentVersion > latestVersion {
		return fmt.Errorf("%w: database=%d supported=%d", ErrSQLiteSchemaTooNew, currentVersion, latestVersion)
	}

	for _, migration := range migrations {
		if migration.version <= currentVersion {
			continue
		}
		if migration.version != currentVersion+1 {
			return fmt.Errorf(
				"invalid SQLite migration sequence: current=%d next=%d",
				currentVersion, migration.version,
			)
		}
		if _, err := connection.ExecContext(ctx, migration.schema); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.version, migration.name, err)
		}
		if _, err := connection.ExecContext(
			ctx, fmt.Sprintf("PRAGMA user_version = %d", migration.version),
		); err != nil {
			return fmt.Errorf("record migration %d (%s): %w", migration.version, migration.name, err)
		}
		currentVersion = migration.version
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit SQLite migrations: %w", err)
	}
	committed = true
	return nil
}

func reconcileSQLiteEventHistory(ctx context.Context, database *sql.DB, maximum int) error {
	var afterProject, afterFingerprint string
	hasCursor := false
	for {
		whereClause := ""
		arguments := make([]any, 0, 4)
		if hasCursor {
			whereClause = "WHERE (project_id, fingerprint) > (?, ?)"
			arguments = append(arguments, afterProject, afterFingerprint)
		}
		arguments = append(arguments, maximum, sqliteHistoryReconcileGroupBatch)
		rows, err := database.QueryContext(ctx, `
SELECT project_id, fingerprint
FROM events
`+whereClause+`
GROUP BY project_id, fingerprint
HAVING COUNT(*) > ?
ORDER BY project_id ASC, fingerprint ASC
LIMIT ?
`, arguments...)
		if err != nil {
			return fmt.Errorf("find oversized event histories: %w", err)
		}
		type issueKey struct {
			projectID   string
			fingerprint string
		}
		groups := make([]issueKey, 0, sqliteHistoryReconcileGroupBatch)
		for rows.Next() {
			var group issueKey
			if err := rows.Scan(&group.projectID, &group.fingerprint); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan oversized event history: %w", err)
			}
			groups = append(groups, group)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate oversized event histories: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close oversized event histories: %w", err)
		}
		if len(groups) == 0 {
			return nil
		}

		transaction, err := database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin event history reconciliation: %w", err)
		}
		for _, group := range groups {
			if _, err := transaction.ExecContext(
				ctx, sqliteTrimEvents, group.projectID, group.fingerprint, maximum,
			); err != nil {
				_ = transaction.Rollback()
				return fmt.Errorf(
					"trim event history for %s/%s: %w",
					group.projectID, group.fingerprint, err,
				)
			}
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit event history reconciliation: %w", err)
		}
		last := groups[len(groups)-1]
		afterProject, afterFingerprint = last.projectID, last.fingerprint
		hasCursor = true
	}
}

func sqliteDataSourceName(path string, connections int) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
		if strings.HasSuffix(path, "?") || strings.HasSuffix(path, "&") {
			separator = ""
		}
	}
	parameters := "_busy_timeout=5000&_foreign_keys=on"
	if connections > 1 {
		parameters += "&_journal_mode=wal"
	}
	return path + separator + parameters
}

func sqliteReadOnlyDataSourceName(path string) string {
	pathPart, rawQuery, hasQuery := strings.Cut(path, "?")
	if !strings.HasPrefix(strings.ToLower(pathPart), "file:") {
		pathPart = (&url.URL{Scheme: "file", Path: pathPart}).String()
	}
	query := make(url.Values)
	if hasQuery {
		if parsed, err := url.ParseQuery(rawQuery); err == nil {
			query = parsed
		}
	}
	query.Set("mode", "ro")
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	return pathPart + "?" + query.Encode()
}

func isMemorySQLitePath(path string) bool {
	lower := strings.ToLower(path)
	if lower == ":memory:" || strings.HasPrefix(lower, "file::memory:") {
		return true
	}
	queryIndex := strings.IndexByte(path, '?')
	if queryIndex < 0 {
		return false
	}
	query, err := url.ParseQuery(path[queryIndex+1:])
	return err == nil && strings.EqualFold(query.Get("mode"), "memory")
}

// Close releases the database connection.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Ready verifies that the connection pool can read the required schema.
func (s *SQLite) Ready(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("SQLite database is unavailable")
	}
	var tables int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_schema
WHERE type = 'table' AND name IN ('issues', 'events')
`).Scan(&tables); err != nil {
		return fmt.Errorf("probe sqlite database: %w", err)
	}
	if tables != 2 {
		return fmt.Errorf("probe sqlite database: found %d of 2 required tables", tables)
	}
	return nil
}

// IntegrityCheck runs SQLite's bounded quick integrity check.
func (s *SQLite) IntegrityCheck(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("SQLite database is unavailable")
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("run SQLite integrity check: %w", err)
	}
	defer rows.Close()

	checked := false
	problems := make([]string, 0)
	for rows.Next() {
		checked = true
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("read SQLite integrity result: %w", err)
		}
		result = strings.TrimSpace(result)
		if !strings.EqualFold(result, "ok") {
			problems = append(problems, result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate SQLite integrity results: %w", err)
	}
	if !checked {
		return fmt.Errorf("%w: no result", ErrIntegrityCheckFailed)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrIntegrityCheckFailed, strings.Join(problems, "; "))
	}
	return nil
}

// Backup creates a consistent snapshot and atomically publishes it at
// destination without replacing an existing filesystem entry.
func (s *SQLite) Backup(ctx context.Context, destination string) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return ErrBackupPathRequired
	}
	if _, err := os.Lstat(destination); err == nil {
		return ErrBackupExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}

	temporary, err := os.CreateTemp(
		filepath.Dir(destination), "."+filepath.Base(destination)+"-*.tmp",
	)
	if err != nil {
		return fmt.Errorf("create temporary backup: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary backup: %w", err)
	}
	defer func() { _ = os.Remove(temporaryPath) }()

	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", temporaryPath); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	backup, err := OpenSQLiteReadOnly(ctx, temporaryPath)
	if err != nil {
		return fmt.Errorf("open SQLite snapshot: %w", err)
	}
	checkErr := backup.IntegrityCheck(ctx)
	closeErr := backup.Close()
	if checkErr != nil {
		return fmt.Errorf("verify SQLite snapshot: %w", checkErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close SQLite snapshot: %w", closeErr)
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if _, statErr := os.Lstat(destination); statErr == nil {
			return ErrBackupExists
		}
		return fmt.Errorf("publish SQLite snapshot: %w", err)
	}
	return nil
}

// Record atomically adds an event to its fingerprint group.
func (s *SQLite) Record(ctx context.Context, projectID string, captured event.Event) (Issue, error) {
	issues, err := s.RecordBatch(ctx, projectID, []event.Event{captured})
	if err != nil {
		return Issue{}, err
	}
	return issues[0], nil
}

type sqliteRecord struct {
	event       event.Event
	fingerprint string
	encoded     []byte
	receivedAt  int64
}

// RecordBatch adds every event in one transaction. Any failed write or read
// rolls the entire batch back, so callers never observe a partial batch.
func (s *SQLite) RecordBatch(ctx context.Context, projectID string, captured []event.Event) ([]Issue, error) {
	projectID, err := validateRecordBatch(ctx, projectID, captured)
	if err != nil {
		return nil, err
	}
	records := make([]sqliteRecord, len(captured))
	for index, item := range captured {
		encoded, encodeErr := json.Marshal(item)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode event %d: %w", index, encodeErr)
		}
		records[index] = sqliteRecord{
			event:       item,
			fingerprint: item.Fingerprint(),
			encoded:     encoded,
			receivedAt:  item.ReceivedAt.UnixNano(),
		}
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin record batch transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	migratedFingerprints := make(map[string]struct{}, len(records))
	for index, record := range records {
		if _, migrated := migratedFingerprints[record.fingerprint]; migrated {
			continue
		}
		if err := migrateLegacyIssueFingerprints(
			ctx, transaction, projectID, record.event, record.fingerprint,
		); err != nil {
			return nil, fmt.Errorf("migrate legacy fingerprint for event %d: %w", index, err)
		}
		migratedFingerprints[record.fingerprint] = struct{}{}
	}

	issueStatement, err := transaction.PrepareContext(ctx, sqliteUpsertIssue)
	if err != nil {
		return nil, fmt.Errorf("prepare issue batch: %w", err)
	}
	eventStatement, err := transaction.PrepareContext(ctx, sqliteInsertEvent)
	if err != nil {
		_ = issueStatement.Close()
		return nil, fmt.Errorf("prepare event batch: %w", err)
	}
	touched := make(map[string]struct{}, len(records))
	for index, record := range records {
		_, err = issueStatement.ExecContext(
			ctx, projectID, record.fingerprint, record.event.Kind,
			record.event.Message, record.event.SourceURL, record.event.Line,
			record.event.Column, IssueStatusOpen, record.receivedAt,
			record.receivedAt, record.encoded,
		)
		if err != nil {
			_ = eventStatement.Close()
			_ = issueStatement.Close()
			return nil, fmt.Errorf("upsert issue for event %d: %w", index, err)
		}
		if _, err = eventStatement.ExecContext(
			ctx, projectID, record.fingerprint, record.event.ID,
			record.receivedAt, record.encoded,
		); err != nil {
			_ = eventStatement.Close()
			_ = issueStatement.Close()
			return nil, fmt.Errorf("insert event history for event %d: %w", index, err)
		}
		touched[record.fingerprint] = struct{}{}
	}
	if err := eventStatement.Close(); err != nil {
		_ = issueStatement.Close()
		return nil, fmt.Errorf("close event batch statement: %w", err)
	}
	if err := issueStatement.Close(); err != nil {
		return nil, fmt.Errorf("close issue batch statement: %w", err)
	}
	for fingerprint := range touched {
		if _, err := transaction.ExecContext(
			ctx, sqliteTrimEvents, projectID, fingerprint, s.maxEventsPerIssue,
		); err != nil {
			return nil, fmt.Errorf("trim event history for %s: %w", fingerprint, err)
		}
	}

	issues := make([]Issue, len(records))
	issuesByFingerprint := make(map[string]Issue, len(records))
	for index, record := range records {
		issue, exists := issuesByFingerprint[record.fingerprint]
		if !exists {
			issue, err = queryIssue(ctx, transaction, projectID, record.fingerprint)
			if err != nil {
				return nil, fmt.Errorf("read issue for event %d: %w", index, err)
			}
			issuesByFingerprint[record.fingerprint] = issue
		}
		issues[index] = issue
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit record batch transaction: %w", err)
	}
	return issues, nil
}

func migrateLegacyIssueFingerprints(
	ctx context.Context,
	transaction *sql.Tx,
	projectID string,
	captured event.Event,
	fingerprint string,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT fingerprint, last_event
FROM issues
WHERE project_id = ? AND fingerprint <> ?
  AND kind = ? AND message = ? AND source_url = ?
  AND line = ? AND column_number = ?
`,
		projectID, fingerprint, captured.Kind, captured.Message,
		captured.SourceURL, captured.Line, captured.Column,
	)
	if err != nil {
		return fmt.Errorf("find legacy issue candidates: %w", err)
	}
	type legacyIssueCandidate struct {
		fingerprint string
		lastEvent   event.Event
	}
	candidates := make([]legacyIssueCandidate, 0)
	for rows.Next() {
		var candidate legacyIssueCandidate
		var encoded []byte
		if err := rows.Scan(&candidate.fingerprint, &encoded); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan legacy issue candidate: %w", err)
		}
		if err := json.Unmarshal(encoded, &candidate.lastEvent); err != nil {
			_ = rows.Close()
			return fmt.Errorf("decode legacy issue candidate %s: %w", candidate.fingerprint, err)
		}
		if candidate.lastEvent.LegacyFingerprint() == candidate.fingerprint &&
			candidate.lastEvent.Fingerprint() == fingerprint {
			candidates = append(candidates, candidate)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate legacy issue candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy issue candidates: %w", err)
	}
	for _, candidate := range candidates {
		if _, err := transaction.ExecContext(
			ctx, sqliteMergeLegacyIssue, fingerprint, projectID, candidate.fingerprint,
		); err != nil {
			return fmt.Errorf("merge legacy issue %s: %w", candidate.fingerprint, err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			"UPDATE events SET fingerprint = ? WHERE project_id = ? AND fingerprint = ?",
			fingerprint, projectID, candidate.fingerprint,
		); err != nil {
			return fmt.Errorf("move legacy event history for %s: %w", candidate.fingerprint, err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			"DELETE FROM issues WHERE project_id = ? AND fingerprint = ?",
			projectID, candidate.fingerprint,
		); err != nil {
			return fmt.Errorf("remove legacy issue %s: %w", candidate.fingerprint, err)
		}
	}
	return nil
}

// GetIssue returns one issue from one project.
func (s *SQLite) GetIssue(ctx context.Context, projectID, fingerprint string) (Issue, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Issue{}, ErrProjectRequired
	}
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	return queryIssue(ctx, s.db, projectID, fingerprint)
}

// ListIssues returns a stable, bounded page ordered by most recently seen.
func (s *SQLite) ListIssues(ctx context.Context, projectID string, options ListOptions) (IssuePage, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return IssuePage{}, ErrProjectRequired
	}
	if err := ctx.Err(); err != nil {
		return IssuePage{}, err
	}
	if options.Status != "" && !options.Status.Valid() {
		return IssuePage{}, ErrInvalidStatus
	}
	options = normalizeListOptions(options)

	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return IssuePage{}, fmt.Errorf("begin list transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	whereClause := "project_id = ?"
	queryArguments := []any{projectID}
	if options.Status != "" {
		whereClause += " AND status = ?"
		queryArguments = append(queryArguments, options.Status)
	}
	countArguments := append([]any{}, queryArguments...)

	var total int
	if err := transaction.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM issues WHERE "+whereClause, countArguments...,
	).Scan(&total); err != nil {
		return IssuePage{}, fmt.Errorf("count issues: %w", err)
	}
	if options.After != nil {
		whereClause += sqliteIssueCursorPredicate
		lastSeen := options.After.LastSeen.UnixNano()
		queryArguments = append(
			queryArguments, lastSeen, lastSeen, lastSeen, options.After.Fingerprint,
		)
	}

	listArguments := append(
		append([]any{}, queryArguments...), options.Limit+1, options.Offset,
	)
	rows, err := transaction.QueryContext(ctx, `
SELECT project_id, fingerprint, kind, message, source_url, line, column_number,
       status, occurrences, first_seen, last_seen, last_event
FROM issues
WHERE `+whereClause+`
ORDER BY last_seen DESC, fingerprint ASC
LIMIT ? OFFSET ?
`, listArguments...)
	if err != nil {
		return IssuePage{}, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()

	issues := make([]Issue, 0, min(options.Limit+1, total))
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return IssuePage{}, err
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return IssuePage{}, fmt.Errorf("iterate issues: %w", err)
	}
	if err := rows.Close(); err != nil {
		return IssuePage{}, fmt.Errorf("close issue rows: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return IssuePage{}, fmt.Errorf("commit list transaction: %w", err)
	}
	var next *ListCursor
	if len(issues) > options.Limit {
		issues = issues[:options.Limit]
		last := issues[len(issues)-1]
		next = &ListCursor{LastSeen: last.LastSeen, Fingerprint: last.Fingerprint}
	}

	return IssuePage{
		Issues: issues,
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
		Next:   next,
	}, nil
}

// ListIssueEvents returns retained occurrences ordered newest first.
func (s *SQLite) ListIssueEvents(
	ctx context.Context,
	projectID, fingerprint string,
	options EventListOptions,
) (EventPage, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return EventPage{}, ErrProjectRequired
	}
	if err := ctx.Err(); err != nil {
		return EventPage{}, err
	}
	options = normalizeEventListOptions(options)

	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EventPage{}, fmt.Errorf("begin event list transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := queryIssue(ctx, transaction, projectID, fingerprint); err != nil {
		return EventPage{}, err
	}

	var total int
	if err := transaction.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM events WHERE project_id = ? AND fingerprint = ?",
		projectID, fingerprint,
	).Scan(&total); err != nil {
		return EventPage{}, fmt.Errorf("count issue events: %w", err)
	}

	whereClause := "project_id = ? AND fingerprint = ?"
	arguments := []any{projectID, fingerprint}
	if options.After != nil {
		whereClause += " AND (received_at < ? OR (received_at = ? AND sequence < ?))"
		receivedAt := options.After.ReceivedAt.UnixNano()
		arguments = append(arguments, receivedAt, receivedAt, options.After.Sequence)
	}
	arguments = append(arguments, options.Limit+1)
	rows, err := transaction.QueryContext(ctx, `
SELECT sequence, received_at, payload
FROM events
WHERE `+whereClause+`
ORDER BY received_at DESC, sequence DESC
LIMIT ?
`, arguments...)
	if err != nil {
		return EventPage{}, fmt.Errorf("list issue events: %w", err)
	}
	defer rows.Close()

	type retainedEvent struct {
		sequence   int64
		receivedAt time.Time
		event      event.Event
	}
	retained := make([]retainedEvent, 0, min(options.Limit+1, total))
	for rows.Next() {
		var (
			item       retainedEvent
			receivedAt int64
			encoded    []byte
		)
		if err := rows.Scan(&item.sequence, &receivedAt, &encoded); err != nil {
			return EventPage{}, fmt.Errorf("scan issue event: %w", err)
		}
		if err := json.Unmarshal(encoded, &item.event); err != nil {
			return EventPage{}, fmt.Errorf("decode issue event: %w", err)
		}
		item.receivedAt = time.Unix(0, receivedAt).UTC()
		retained = append(retained, item)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, fmt.Errorf("iterate issue events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return EventPage{}, fmt.Errorf("close issue event rows: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return EventPage{}, fmt.Errorf("commit event list transaction: %w", err)
	}

	var next *EventCursor
	if len(retained) > options.Limit {
		retained = retained[:options.Limit]
		last := retained[len(retained)-1]
		next = &EventCursor{ReceivedAt: last.receivedAt, Sequence: last.sequence}
	}
	events := make([]event.Event, len(retained))
	for index, item := range retained {
		events[index] = item.event
	}
	return EventPage{Events: events, Total: total, Limit: options.Limit, Next: next}, nil
}

// SetIssueStatus updates the triage state of one issue.
func (s *SQLite) SetIssueStatus(ctx context.Context, projectID, fingerprint string, status IssueStatus) (Issue, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Issue{}, ErrProjectRequired
	}
	if !status.Valid() {
		return Issue{}, ErrInvalidStatus
	}
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Issue{}, fmt.Errorf("begin status transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
UPDATE issues SET status = ? WHERE project_id = ? AND fingerprint = ?
`, status, projectID, fingerprint)
	if err != nil {
		return Issue{}, fmt.Errorf("update issue status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Issue{}, fmt.Errorf("read updated row count: %w", err)
	}
	if rowsAffected == 0 {
		return Issue{}, ErrIssueNotFound
	}

	issue, err := queryIssue(ctx, transaction, projectID, fingerprint)
	if err != nil {
		return Issue{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Issue{}, fmt.Errorf("commit status transaction: %w", err)
	}
	return issue, nil
}

// PruneIssues atomically removes at most PruneBatchSize issues in one project
// that have not been seen since the cutoff. SQLite can reuse the released pages
// for future writes.
func (s *SQLite) PruneIssues(ctx context.Context, projectID string, cutoff time.Time) (int64, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, ErrProjectRequired
	}
	if cutoff.IsZero() {
		return 0, ErrCutoffRequired
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM issues WHERE rowid IN (
    SELECT rowid FROM issues
    WHERE project_id = ? AND last_seen < ?
    ORDER BY last_seen ASC
    LIMIT ?
)`,
		projectID,
		cutoff.UnixNano(),
		PruneBatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("prune issues: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read pruned issue count: %w", err)
	}
	return deleted, nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func queryIssue(ctx context.Context, querier rowQuerier, projectID, fingerprint string) (Issue, error) {
	row := querier.QueryRowContext(ctx, `
SELECT project_id, fingerprint, kind, message, source_url, line, column_number,
       status, occurrences, first_seen, last_seen, last_event
FROM issues
WHERE project_id = ? AND fingerprint = ?
`, projectID, fingerprint)
	issue, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, ErrIssueNotFound
	}
	return issue, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanIssue(scanner rowScanner) (Issue, error) {
	var (
		issue       Issue
		occurrences int64
		firstSeen   int64
		lastSeen    int64
		encoded     []byte
	)
	if err := scanner.Scan(
		&issue.ProjectID, &issue.Fingerprint, &issue.Kind, &issue.Message,
		&issue.SourceURL, &issue.Line, &issue.Column, &issue.Status,
		&occurrences, &firstSeen, &lastSeen, &encoded,
	); err != nil {
		return Issue{}, err
	}
	if occurrences < 0 {
		return Issue{}, fmt.Errorf("decode issue: negative occurrence count")
	}
	if err := json.Unmarshal(encoded, &issue.LastEvent); err != nil {
		return Issue{}, fmt.Errorf("decode last event: %w", err)
	}
	issue.Occurrences = uint64(occurrences)
	issue.FirstSeen = time.Unix(0, firstSeen).UTC()
	issue.LastSeen = time.Unix(0, lastSeen).UTC()
	return issue, nil
}
