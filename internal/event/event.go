// Package event defines the versioned data contract used by Error-Tracer.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	maxMessageLength     = 4 * 1024
	maxStackLength       = 64 * 1024
	maxURLLength         = 2 * 1024
	maxReleaseLength     = 128
	maxEnvironmentLength = 128
	maxUserAgentLength   = 1024
	maxTags              = 32
	maxTagKeyLength      = 64
	maxTagValueLength    = 256
)

// Kind identifies the source of a captured client-side failure.
type Kind string

const (
	KindError              Kind = "error"
	KindUnhandledRejection Kind = "unhandled_rejection"
	KindResourceError      Kind = "resource_error"
)

// Event is the canonical representation of one captured failure.
// ID, ReceivedAt, and UserAgent are assigned or overwritten by the collector.
type Event struct {
	ID          string            `json:"id,omitempty"`
	Kind        Kind              `json:"kind"`
	Message     string            `json:"message,omitempty"`
	Stack       string            `json:"stack,omitempty"`
	SourceURL   string            `json:"source_url,omitempty"`
	PageURL     string            `json:"page_url,omitempty"`
	Line        int               `json:"line,omitempty"`
	Column      int               `json:"column,omitempty"`
	OccurredAt  *time.Time        `json:"occurred_at,omitempty"`
	ReceivedAt  time.Time         `json:"received_at,omitempty"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// ValidationError identifies a client-controlled field that violates the
// event contract.
type ValidationError struct {
	Field   string
	Problem string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

// Normalize removes surrounding whitespace, strips URL credentials and query
// data, and normalizes tag keys before validation and fingerprinting.
func (e *Event) Normalize() {
	e.Message = strings.TrimSpace(e.Message)
	e.Stack = strings.TrimSpace(e.Stack)
	e.SourceURL = sanitizeURL(e.SourceURL)
	e.PageURL = sanitizeURL(e.PageURL)
	e.Release = strings.TrimSpace(e.Release)
	e.Environment = strings.TrimSpace(e.Environment)
	e.UserAgent = strings.TrimSpace(e.UserAgent)

	if len(e.Tags) == 0 {
		e.Tags = nil
		return
	}

	tags := make(map[string]string, len(e.Tags))
	for key, value := range e.Tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = strings.TrimSpace(value)
	}
	if len(tags) == 0 {
		tags = nil
	}
	e.Tags = tags
}

// Validate checks the normalized event against bounded ingestion rules.
func (e Event) Validate() error {
	if !e.Kind.valid() {
		return invalid("kind", "must be error, unhandled_rejection, or resource_error")
	}
	if e.Kind != KindResourceError && e.Message == "" {
		return invalid("message", "is required")
	}
	if e.Kind == KindResourceError && e.SourceURL == "" {
		return invalid("source_url", "is required for resource errors")
	}
	if err := bounded("message", e.Message, maxMessageLength); err != nil {
		return err
	}
	if err := bounded("stack", e.Stack, maxStackLength); err != nil {
		return err
	}
	if err := bounded("source_url", e.SourceURL, maxURLLength); err != nil {
		return err
	}
	if err := bounded("page_url", e.PageURL, maxURLLength); err != nil {
		return err
	}
	if err := bounded("release", e.Release, maxReleaseLength); err != nil {
		return err
	}
	if err := bounded("environment", e.Environment, maxEnvironmentLength); err != nil {
		return err
	}
	if err := bounded("user_agent", e.UserAgent, maxUserAgentLength); err != nil {
		return err
	}
	if e.Line < 0 {
		return invalid("line", "must not be negative")
	}
	if e.Column < 0 {
		return invalid("column", "must not be negative")
	}
	if len(e.Tags) > maxTags {
		return invalid("tags", fmt.Sprintf("must contain at most %d entries", maxTags))
	}
	for key, value := range e.Tags {
		if key == "" {
			return invalid("tags", "keys must not be empty")
		}
		if len(key) > maxTagKeyLength {
			return invalid("tags", fmt.Sprintf("key %q exceeds %d bytes", key, maxTagKeyLength))
		}
		if len(value) > maxTagValueLength {
			return invalid("tags", fmt.Sprintf("value for %q exceeds %d bytes", key, maxTagValueLength))
		}
	}
	return nil
}

// Fingerprint returns a stable issue grouping key. It deliberately excludes
// release, environment, page URL, user agent, and tags so the same underlying
// failure remains grouped across deployments and clients.
func (e Event) Fingerprint() string {
	parts := []string{
		"error-tracer-v1",
		string(e.Kind),
		e.Message,
		e.SourceURL,
		fmt.Sprintf("%d", e.Line),
		fmt.Sprintf("%d", e.Column),
		firstStackLine(e.Stack),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func (kind Kind) valid() bool {
	switch kind {
	case KindError, KindUnhandledRejection, KindResourceError:
		return true
	default:
		return false
	}
}

func sanitizeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func firstStackLine(stack string) string {
	for line := range strings.SplitSeq(stack, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func bounded(field, value string, limit int) error {
	if len(value) > limit {
		return invalid(field, fmt.Sprintf("must not exceed %d bytes", limit))
	}
	return nil
}

func invalid(field, problem string) error {
	return &ValidationError{Field: field, Problem: problem}
}
