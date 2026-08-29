package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
	_ "modernc.org/sqlite"
)

const sqliteSchema = `
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
`

const sqliteUpsertIssue = `
INSERT INTO issues (
    project_id, fingerprint, kind, message, source_url, line, column_number,
    status, occurrences, first_seen, last_seen, last_event
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT (project_id, fingerprint) DO UPDATE SET
    occurrences = issues.occurrences + 1,
    first_seen = MIN(issues.first_seen, excluded.first_seen),
    last_event = CASE
        WHEN excluded.last_seen >= issues.last_seen THEN excluded.last_event
        ELSE issues.last_event
    END,
    last_seen = MAX(issues.last_seen, excluded.last_seen)
`

var ErrDatabasePathRequired = errors.New("database path is required")

// SQLite persists aggregated issues in a local SQLite database.
type SQLite struct {
	db *sql.DB
}

// OpenSQLite opens a SQLite database and applies its idempotent schema.
func OpenSQLite(ctx context.Context, path string) (*SQLite, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrDatabasePathRequired
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// A single connection makes :memory: databases deterministic and avoids
	// lock contention while writes are serialized by SQLite.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	fail := func(operation string, operationErr error) (*SQLite, error) {
		_ = database.Close()
		return nil, fmt.Errorf("%s: %w", operation, operationErr)
	}

	if err := database.PingContext(ctx); err != nil {
		return fail("ping sqlite database", err)
	}
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fail("configure sqlite database", err)
		}
	}
	if _, err := database.ExecContext(ctx, sqliteSchema); err != nil {
		return fail("migrate sqlite database", err)
	}

	return &SQLite{db: database}, nil
}

// Close releases the database connection.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
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

	statement, err := transaction.PrepareContext(ctx, sqliteUpsertIssue)
	if err != nil {
		return nil, fmt.Errorf("prepare issue batch: %w", err)
	}
	for index, record := range records {
		_, err = statement.ExecContext(
			ctx, projectID, record.fingerprint, record.event.Kind,
			record.event.Message, record.event.SourceURL, record.event.Line,
			record.event.Column, IssueStatusOpen, record.receivedAt,
			record.receivedAt, record.encoded,
		)
		if err != nil {
			_ = statement.Close()
			return nil, fmt.Errorf("upsert issue for event %d: %w", index, err)
		}
	}
	if err := statement.Close(); err != nil {
		return nil, fmt.Errorf("close issue batch statement: %w", err)
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

	var total int
	if err := transaction.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM issues WHERE "+whereClause, queryArguments...,
	).Scan(&total); err != nil {
		return IssuePage{}, fmt.Errorf("count issues: %w", err)
	}

	listArguments := append(append([]any{}, queryArguments...), options.Limit, options.Offset)
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

	issues := make([]Issue, 0, min(options.Limit, total))
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

	return IssuePage{
		Issues: issues,
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
	}, nil
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

// PruneIssues atomically removes issues in one project that have not been seen
// since the cutoff. SQLite can reuse the released pages for future writes.
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
		"DELETE FROM issues WHERE project_id = ? AND last_seen < ?",
		projectID,
		cutoff.UnixNano(),
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
