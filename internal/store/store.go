// Package store defines persistence contracts for captured events and issues.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/soulteary/Error-Tracer/internal/event"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

var (
	ErrIssueNotFound   = errors.New("issue not found")
	ErrInvalidStatus   = errors.New("invalid issue status")
	ErrProjectRequired = errors.New("project ID is required")
	ErrEventsRequired  = errors.New("at least one event is required")
	ErrReceivedAtEmpty = errors.New("event received_at is required")
	ErrCutoffRequired  = errors.New("retention cutoff is required")
)

// IssueStatus describes the triage state of an aggregated issue.
type IssueStatus string

const (
	IssueStatusOpen     IssueStatus = "open"
	IssueStatusResolved IssueStatus = "resolved"
	IssueStatusIgnored  IssueStatus = "ignored"
)

// Valid reports whether the status can be persisted.
func (status IssueStatus) Valid() bool {
	switch status {
	case IssueStatusOpen, IssueStatusResolved, IssueStatusIgnored:
		return true
	default:
		return false
	}
}

// Issue is the server-side aggregation of events with the same fingerprint.
type Issue struct {
	ProjectID   string      `json:"-"`
	Fingerprint string      `json:"fingerprint"`
	Kind        event.Kind  `json:"kind"`
	Message     string      `json:"message,omitempty"`
	SourceURL   string      `json:"source_url,omitempty"`
	Line        int         `json:"line,omitempty"`
	Column      int         `json:"column,omitempty"`
	Status      IssueStatus `json:"status"`
	Occurrences uint64      `json:"occurrences"`
	FirstSeen   time.Time   `json:"first_seen"`
	LastSeen    time.Time   `json:"last_seen"`
	LastEvent   event.Event `json:"last_event"`
}

// ListOptions controls bounded issue pagination.
type ListOptions struct {
	Limit  int
	Offset int
	Status IssueStatus
	After  *ListCursor
}

// ListCursor identifies the last issue returned by a stable ordered page.
type ListCursor struct {
	LastSeen    time.Time
	Fingerprint string
}

// IssuePage is a stable page of issues and the total matching count.
type IssuePage struct {
	Issues []Issue     `json:"issues"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
	Next   *ListCursor `json:"-"`
}

// Store records events and exposes their aggregated issues.
type Store interface {
	Record(context.Context, string, event.Event) (Issue, error)
	RecordBatch(context.Context, string, []event.Event) ([]Issue, error)
	GetIssue(context.Context, string, string) (Issue, error)
	ListIssues(context.Context, string, ListOptions) (IssuePage, error)
	SetIssueStatus(context.Context, string, string, IssueStatus) (Issue, error)
}

func normalizeListOptions(options ListOptions) ListOptions {
	if options.Limit <= 0 {
		options.Limit = defaultPageSize
	}
	if options.Limit > maxPageSize {
		options.Limit = maxPageSize
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	if options.After != nil {
		options.Offset = 0
	}
	return options
}

func validateRecordBatch(ctx context.Context, projectID string, captured []event.Event) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", ErrProjectRequired
	}
	if len(captured) == 0 {
		return "", ErrEventsRequired
	}
	for _, item := range captured {
		if item.ReceivedAt.IsZero() {
			return "", ErrReceivedAtEmpty
		}
	}
	return projectID, nil
}
