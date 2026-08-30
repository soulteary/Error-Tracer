package store

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/soulteary/Error-Tracer/internal/event"
)

// Memory is a concurrency-safe in-memory store for development and tests.
type Memory struct {
	mu     sync.RWMutex
	issues map[string]Issue
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{issues: make(map[string]Issue)}
}

// Record adds an event to its fingerprint group.
func (m *Memory) Record(ctx context.Context, projectID string, captured event.Event) (Issue, error) {
	issues, err := m.RecordBatch(ctx, projectID, []event.Event{captured})
	if err != nil {
		return Issue{}, err
	}
	return issues[0], nil
}

// RecordBatch atomically adds a non-empty batch of events.
func (m *Memory) RecordBatch(ctx context.Context, projectID string, captured []event.Event) ([]Issue, error) {
	projectID, err := validateRecordBatch(ctx, projectID, captured)
	if err != nil {
		return nil, err
	}
	events := make([]event.Event, len(captured))
	for index, item := range captured {
		events[index] = cloneEvent(item)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	fingerprints := make([]string, len(events))
	for index, item := range events {
		fingerprint := item.Fingerprint()
		fingerprints[index] = fingerprint
		key := issueKey(projectID, fingerprint)
		issue, exists := m.issues[key]
		if !exists {
			issue = Issue{
				ProjectID:   projectID,
				Fingerprint: fingerprint,
				Kind:        item.Kind,
				Message:     item.Message,
				SourceURL:   item.SourceURL,
				Line:        item.Line,
				Column:      item.Column,
				Status:      IssueStatusOpen,
				FirstSeen:   item.ReceivedAt,
				LastSeen:    item.ReceivedAt,
				LastEvent:   item,
			}
		} else if issue.Status == IssueStatusResolved {
			issue.Status = IssueStatusOpen
		}

		issue.Occurrences++
		if item.ReceivedAt.Before(issue.FirstSeen) {
			issue.FirstSeen = item.ReceivedAt
		}
		if !item.ReceivedAt.Before(issue.LastSeen) {
			issue.LastSeen = item.ReceivedAt
			issue.LastEvent = item
		}
		m.issues[key] = issue
	}
	issues := make([]Issue, len(events))
	for index, fingerprint := range fingerprints {
		issues[index] = cloneIssue(m.issues[issueKey(projectID, fingerprint)])
	}
	return issues, nil
}

// GetIssue returns one issue from one project.
func (m *Memory) GetIssue(ctx context.Context, projectID, fingerprint string) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Issue{}, ErrProjectRequired
	}

	m.mu.RLock()
	issue, exists := m.issues[issueKey(projectID, fingerprint)]
	m.mu.RUnlock()
	if !exists {
		return Issue{}, ErrIssueNotFound
	}
	return cloneIssue(issue), nil
}

// ListIssues returns issues ordered by most recently seen, then fingerprint.
func (m *Memory) ListIssues(ctx context.Context, projectID string, options ListOptions) (IssuePage, error) {
	if err := ctx.Err(); err != nil {
		return IssuePage{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return IssuePage{}, ErrProjectRequired
	}
	if options.Status != "" && !options.Status.Valid() {
		return IssuePage{}, ErrInvalidStatus
	}
	options = normalizeListOptions(options)

	m.mu.RLock()
	issues := make([]Issue, 0, len(m.issues))
	for _, issue := range m.issues {
		if issue.ProjectID == projectID &&
			(options.Status == "" || issue.Status == options.Status) {
			issues = append(issues, cloneIssue(issue))
		}
	}
	m.mu.RUnlock()

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].LastSeen.Equal(issues[j].LastSeen) {
			return issues[i].Fingerprint < issues[j].Fingerprint
		}
		return issues[i].LastSeen.After(issues[j].LastSeen)
	})

	total := len(issues)
	start := min(options.Offset, total)
	if options.After != nil {
		start = sort.Search(total, func(index int) bool {
			return issueIsAfterCursor(issues[index], *options.After)
		})
	}
	end := min(start+options.Limit, total)
	var next *ListCursor
	if end < total && end > start {
		last := issues[end-1]
		next = &ListCursor{LastSeen: last.LastSeen, Fingerprint: last.Fingerprint}
	}
	return IssuePage{
		Issues: issues[start:end],
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
		Next:   next,
	}, nil
}

func issueIsAfterCursor(issue Issue, cursor ListCursor) bool {
	return issue.LastSeen.Before(cursor.LastSeen) ||
		(issue.LastSeen.Equal(cursor.LastSeen) && issue.Fingerprint > cursor.Fingerprint)
}

// SetIssueStatus updates the triage state of one issue.
func (m *Memory) SetIssueStatus(ctx context.Context, projectID, fingerprint string, status IssueStatus) (Issue, error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Issue{}, ErrProjectRequired
	}
	if !status.Valid() {
		return Issue{}, ErrInvalidStatus
	}

	key := issueKey(projectID, fingerprint)
	m.mu.Lock()
	defer m.mu.Unlock()
	issue, exists := m.issues[key]
	if !exists {
		return Issue{}, ErrIssueNotFound
	}
	issue.Status = status
	m.issues[key] = issue
	return cloneIssue(issue), nil
}

func issueKey(projectID, fingerprint string) string {
	return projectID + "\x00" + fingerprint
}

func cloneIssue(issue Issue) Issue {
	issue.LastEvent = cloneEvent(issue.LastEvent)
	return issue
}

func cloneEvent(captured event.Event) event.Event {
	if captured.OccurredAt != nil {
		occurredAt := *captured.OccurredAt
		captured.OccurredAt = &occurredAt
	}
	if captured.Tags != nil {
		tags := make(map[string]string, len(captured.Tags))
		for key, value := range captured.Tags {
			tags[key] = value
		}
		captured.Tags = tags
	}
	return captured
}
