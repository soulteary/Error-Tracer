// Package event defines the versioned data contract used by Error-Tracer.
package event

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
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

	keys := make([]string, 0, len(e.Tags))
	for key := range e.Tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	tags := make(map[string]string, len(e.Tags))
	exactKeys := make(map[string]bool, len(e.Tags))
	for _, originalKey := range keys {
		key := strings.TrimSpace(originalKey)
		if key == "" {
			continue
		}
		if originalKey == key {
			tags[key] = strings.TrimSpace(e.Tags[originalKey])
			exactKeys[key] = true
			continue
		}
		if _, exists := tags[key]; !exists && !exactKeys[key] {
			tags[key] = strings.TrimSpace(e.Tags[originalKey])
		}
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
		"error-tracer-v2",
		string(e.Kind),
		e.Message,
		e.SourceURL,
		fmt.Sprintf("%d", e.Line),
		fmt.Sprintf("%d", e.Column),
		firstStackFrame(e.Stack),
	}
	hasher := sha256.New()
	var length [binary.MaxVarintLen64]byte
	for _, part := range parts {
		size := binary.PutUvarint(length[:], uint64(len(part)))
		_, _ = hasher.Write(length[:size])
		_, _ = hasher.Write([]byte(part))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// LegacyFingerprint returns the v1 grouping key used by existing databases.
// Persistent stores use it only to carry an issue's state and retained history
// forward when that issue first recurs under the v2 fingerprint algorithm.
func (e Event) LegacyFingerprint() string {
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
		// Never persist an unparseable client-controlled URL. In particular,
		// malformed percent escapes can otherwise preserve userinfo, queries,
		// and fragments that the normal parsed path removes.
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func firstStackFrame(stack string) string {
	firstLine := ""
	for line := range strings.SplitSeq(stack, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if firstLine == "" {
			firstLine = line
		}
		if strings.HasPrefix(line, "at ") {
			return canonicalizeStackFrame(line)
		}
		if firefoxStackFramePattern.MatchString(line) {
			return canonicalizeStackFrame(line)
		}
	}
	return firstLine
}

func firstStackLine(stack string) string {
	for line := range strings.SplitSeq(stack, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

var firefoxStackFramePattern = regexp.MustCompile(`^[^@]*@\S+:\d+(?::\d+)?$`)

func canonicalizeStackFrame(frame string) string {
	if strings.HasPrefix(frame, "at ") {
		if open := strings.LastIndexByte(frame, '('); open >= 3 && strings.HasSuffix(frame, ")") {
			return frame[:open+1] + canonicalizeStackLocation(frame[open+1:len(frame)-1]) + ")"
		}
		return "at " + canonicalizeStackLocation(strings.TrimSpace(strings.TrimPrefix(frame, "at ")))
	}

	at := strings.IndexByte(frame, '@')
	if at < 0 {
		return frame
	}
	return frame[:at+1] + canonicalizeStackLocation(frame[at+1:])
}

var stackPositionPattern = regexp.MustCompile(`:\d+(?::\d+)?$`)

func canonicalizeStackLocation(location string) string {
	position := ""
	if match := stackPositionPattern.FindStringIndex(location); match != nil {
		position = location[match[0]:]
		location = location[:match[0]]
	}
	if index := strings.IndexAny(location, "?#"); index >= 0 {
		location = location[:index]
	}
	return location + position
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
