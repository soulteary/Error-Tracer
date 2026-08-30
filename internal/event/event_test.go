package event

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeRemovesSensitiveURLComponents(t *testing.T) {
	event := Event{
		Message:   "  failed  ",
		Stack:     "  at run (app.js:10:2)  ",
		SourceURL: " https://user:secret@example.com/app.js?token=secret#fragment ",
		PageURL:   "https://example.com/page?session=secret#section",
		Tags: map[string]string{
			" feature ": " checkout ",
			"   ":       "discarded",
		},
	}

	event.Normalize()

	if event.Message != "failed" {
		t.Fatalf("Message = %q, want %q", event.Message, "failed")
	}
	if event.Stack != "at run (app.js:10:2)" {
		t.Fatalf("Stack = %q, want trimmed stack", event.Stack)
	}
	if event.SourceURL != "https://example.com/app.js" {
		t.Fatalf("SourceURL = %q, want sanitized source URL", event.SourceURL)
	}
	if event.PageURL != "https://example.com/page" {
		t.Fatalf("PageURL = %q, want sanitized page URL", event.PageURL)
	}
	if len(event.Tags) != 1 || event.Tags["feature"] != "checkout" {
		t.Fatalf("Tags = %#v, want normalized tags", event.Tags)
	}
}

func TestNormalizeDropsMalformedURLs(t *testing.T) {
	captured := Event{
		Kind:      KindError,
		Message:   "failed",
		SourceURL: "https://user:secret@example.com/%zz?token=secret#fragment",
		PageURL:   "https://example.com/%zz?session=secret#fragment",
	}

	captured.Normalize()

	if captured.SourceURL != "" || captured.PageURL != "" {
		t.Fatalf(
			"malformed URLs survived normalization: source=%q page=%q",
			captured.SourceURL, captured.PageURL,
		)
	}
	if err := captured.Validate(); err != nil {
		t.Fatalf("Validate() returned %v after dropping optional malformed URLs", err)
	}
}

func TestNormalizeRejectsMalformedResourceURL(t *testing.T) {
	captured := Event{
		Kind:      KindResourceError,
		SourceURL: "https://user:secret@example.com/%zz?token=secret#fragment",
	}
	captured.Normalize()

	err := captured.Validate()
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "source_url" {
		t.Fatalf("Validate() error = %v, want source_url validation error", err)
	}
}

func TestNormalizeResolvesTagKeyCollisionsDeterministically(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want string
	}{
		{
			name: "exact key wins",
			tags: map[string]string{
				" role": "leading",
				"role":  "exact",
				"role ": "trailing",
			},
			want: "exact",
		},
		{
			name: "first alias wins without exact key",
			tags: map[string]string{
				" role": "leading",
				"role ": "trailing",
			},
			want: "leading",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for range 100 {
				captured := Event{Tags: test.tags}
				captured.Normalize()
				if len(captured.Tags) != 1 || captured.Tags["role"] != test.want {
					t.Fatalf("Tags = %#v, want role=%q", captured.Tags, test.want)
				}
			}
		})
	}
}

func TestValidateAcceptsSupportedEvents(t *testing.T) {
	tests := []Event{
		{Kind: KindError, Message: "boom"},
		{Kind: KindUnhandledRejection, Message: "rejected"},
		{Kind: KindResourceError, SourceURL: "https://example.com/app.js"},
	}

	for _, event := range tests {
		if err := event.Validate(); err != nil {
			t.Fatalf("Validate(%q) returned %v", event.Kind, err)
		}
	}
}

func TestValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		field string
	}{
		{name: "kind", event: Event{Kind: "other", Message: "boom"}, field: "kind"},
		{name: "message", event: Event{Kind: KindError}, field: "message"},
		{name: "resource source", event: Event{Kind: KindResourceError}, field: "source_url"},
		{name: "message length", event: Event{Kind: KindError, Message: strings.Repeat("x", maxMessageLength+1)}, field: "message"},
		{name: "negative line", event: Event{Kind: KindError, Message: "boom", Line: -1}, field: "line"},
		{name: "negative column", event: Event{Kind: KindError, Message: "boom", Column: -1}, field: "column"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.event.Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
			if validationErr.Field != test.field {
				t.Fatalf("field = %q, want %q", validationErr.Field, test.field)
			}
		})
	}
}

func TestFingerprintUsesSourceAndLocation(t *testing.T) {
	base := Event{
		Kind:      KindError,
		Message:   "undefined is not a function",
		SourceURL: "https://cdn.example.com/app.js",
		Line:      10,
		Column:    2,
		Stack:     "at run (app.js:10:2)\nat main (app.js:20:1)",
	}

	same := base
	same.Release = "2.0.0"
	same.Environment = "staging"
	same.PageURL = "https://example.com/checkout"
	if base.Fingerprint() != same.Fingerprint() {
		t.Fatal("deployment metadata unexpectedly changed the fingerprint")
	}

	differentSource := base
	differentSource.SourceURL = "https://cdn.example.com/other.js"
	if base.Fingerprint() == differentSource.Fingerprint() {
		t.Fatal("different source files produced the same fingerprint")
	}

	differentLine := base
	differentLine.Line++
	if base.Fingerprint() == differentLine.Fingerprint() {
		t.Fatal("different source locations produced the same fingerprint")
	}
}

func TestFingerprintUsesFirstStackFrame(t *testing.T) {
	first := Event{
		Kind:    KindUnhandledRejection,
		Message: "request failed",
		Stack:   "Error: request failed\n    at checkout (app.js:10:2)",
	}
	second := first
	second.Stack = "Error: request failed\n    at payment (app.js:40:7)"

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("different first stack frames produced the same fingerprint")
	}

	firefox := first
	firefox.Stack = "checkout@https://example.com/app.js:10:2\nmain@https://example.com/app.js:20:1"
	otherFirefox := firefox
	otherFirefox.Stack = "payment@https://example.com/app.js:40:7"
	if firefox.Fingerprint() == otherFirefox.Fingerprint() {
		t.Fatal("different Firefox stack frames produced the same fingerprint")
	}

	safari := first
	safari.Stack = "global code@https://example.com/app.js:10:2"
	otherSafari := safari
	otherSafari.Stack = "global code@https://example.com/app.js:40:7"
	if safari.Fingerprint() == otherSafari.Fingerprint() {
		t.Fatal("different Safari stack frames produced the same fingerprint")
	}
}

func TestFingerprintSkipsAtSignInErrorHeader(t *testing.T) {
	first := Event{
		Kind:    KindError,
		Message: "request failed",
		Stack:   "Error: request for alice@example.com failed\n    at checkout (app.js:10:2)",
	}
	second := first
	second.Stack = "Error: request for alice@example.com failed\n    at payment (app.js:40:7)"

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("an at-sign in the error header hid different V8 stack frames")
	}
}

func TestFingerprintSkipsNumericEmailSuffixInErrorHeader(t *testing.T) {
	first := Event{
		Kind:    KindError,
		Message: "connection failed",
		Stack:   "Error: connect alice@example.com:443\n    at checkout (app.js:10:2)",
	}
	second := first
	second.Stack = "Error: connect alice@example.com:443\n    at payment (app.js:40:7)"

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("a numeric email suffix in the error header hid different V8 stack frames")
	}
}

func TestFingerprintAcceptsAtSignsInsideFirefoxFrameURLs(t *testing.T) {
	first := Event{
		Kind:    KindUnhandledRejection,
		Message: "request failed",
		Stack: "Error: request failed\n" +
			"run@webpack:///node_modules/@scope/pkg/app.js:10:2",
	}
	second := first
	second.Stack = "Error: request failed\n" +
		"run@webpack:///node_modules/@scope/pkg/worker.js:40:7"

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("an at-sign inside a Firefox frame URL hid different call sites")
	}
	cacheVariant := first
	cacheVariant.Stack = "Error: request failed\n" +
		"run@webpack:///node_modules/@scope/pkg/app.js?v=2:10:2"
	first.Stack = "Error: request failed\n" +
		"run@webpack:///node_modules/@scope/pkg/app.js?v=1:10:2"
	if first.Fingerprint() != cacheVariant.Fingerprint() {
		t.Fatal("a cache-busting scoped-package frame URL changed the fingerprint")
	}
}

func TestFingerprintSkipsV8PseudoFramesWithoutCoordinates(t *testing.T) {
	first := Event{
		Kind:    KindUnhandledRejection,
		Message: "request failed",
		Stack: "Error: request failed\n" +
			"    at async Promise.all (index 0)\n" +
			"    at checkout (/app.js:10:2)",
	}
	second := first
	second.Stack = "Error: request failed\n" +
		"    at new Promise (<anonymous>)\n" +
		"    at payment (/app.js:40:7)"

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("coordinate-free V8 pseudo-frames hid different source call sites")
	}
}

func TestFingerprintCanonicalizesStackFrameURL(t *testing.T) {
	first := Event{
		Kind:    KindError,
		Message: "boom",
		Stack:   "Error: boom\n    at run (https://example.com/app.js?v=1#old:10:2)",
	}
	second := first
	second.Stack = "Error: boom\n    at run (https://example.com/app.js?v=2#new:10:2)"

	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("a cache-busting stack-frame URL changed the fingerprint")
	}

	first.Stack = "run@https://example.com/app.js?v=1#old:10:2"
	second.Stack = "run@https://example.com/app.js?v=2#new:10:2"
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("a cache-busting Firefox stack-frame URL changed the fingerprint")
	}
}

func TestFingerprintEncodesFieldBoundaries(t *testing.T) {
	first := Event{Kind: KindError, Message: "a\x00b", SourceURL: "c"}
	second := Event{Kind: KindError, Message: "a", SourceURL: "b\x00c"}

	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("ambiguous field boundaries produced the same fingerprint")
	}
}

func TestLegacyFingerprintMatchesTheV1Encoding(t *testing.T) {
	captured := Event{
		Kind:      KindError,
		Message:   "boom",
		SourceURL: "https://example.com/app.js",
		Line:      10,
		Column:    2,
		Stack:     "Error: boom\n    at run (app.js:10:2)",
	}

	const want = "70f12ea2361e3f43b73102b04e669161acc3b4283eca455e262ea35d6e9a4e3b"
	if got := captured.LegacyFingerprint(); got != want {
		t.Fatalf("LegacyFingerprint() = %q, want v1 digest %q", got, want)
	}
}

func TestFingerprintIgnoresURLQueryAfterNormalization(t *testing.T) {
	first := Event{Kind: KindError, Message: "boom", SourceURL: "https://example.com/app.js?v=1"}
	second := Event{Kind: KindError, Message: "boom", SourceURL: "https://example.com/app.js?v=2"}
	first.Normalize()
	second.Normalize()

	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("cache-busting query changed the fingerprint")
	}
}
