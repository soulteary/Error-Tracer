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
	if err := ctx.Err(); err != nil {
		return Issue{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Issue{}, ErrProjectRequired
	}
	if captured.ReceivedAt.IsZero() {
		return Issue{}, ErrReceivedAtEmpty
	}

	fingerprint := captured.Fingerprint()
	key := issueKey(projectID, fingerprint)
	captured = cloneEvent(captured)

	m.mu.Lock()
	defer m.mu.Unlock()

	issue, exists := m.issues[key]
	if !exists {
		issue = Issue{
			ProjectID:   projectID,
			Fingerprint: fingerprint,
			Kind:        captured.Kind,
			Message:     captured.Message,
			SourceURL:   captured.SourceURL,
			Line:        captured.Line,
			Column:      captured.Column,
			Status:      IssueStatusOpen,
			FirstSeen:   captured.ReceivedAt,
			LastSeen:    captured.ReceivedAt,
			LastEvent:   captured,
		}
	}

	issue.Occurrences++
	if captured.ReceivedAt.Before(issue.FirstSeen) {
		issue.FirstSeen = captured.ReceivedAt
	}
	if !captured.ReceivedAt.Before(issue.LastSeen) {
		issue.LastSeen = captured.ReceivedAt
		issue.LastEvent = captured
	}
	m.issues[key] = issue
	return cloneIssue(issue), nil
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
	options = normalizeListOptions(options)

	m.mu.RLock()
	issues := make([]Issue, 0, len(m.issues))
	for _, issue := range m.issues {
		if issue.ProjectID == projectID {
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
	end := min(start+options.Limit, total)
	return IssuePage{
		Issues: issues[start:end],
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
	}, nil
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
