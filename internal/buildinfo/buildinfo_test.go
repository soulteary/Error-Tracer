package buildinfo

import "testing"

func TestCurrentAndSummaryNormalizeBuildValues(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := version, commit, builtAt
	t.Cleanup(func() {
		version, commit, builtAt = originalVersion, originalCommit, originalBuiltAt
	})
	version = " 2.0.0 "
	commit = " abc123 "
	builtAt = " 2026-08-30T00:00:00Z "

	info := Current()
	if info.Version != "2.0.0" || info.Commit != "abc123" || info.BuiltAt != "2026-08-30T00:00:00Z" {
		t.Fatalf("Current() = %+v", info)
	}
	if got := Summary(); got != "error-tracer 2.0.0 (commit abc123, built 2026-08-30T00:00:00Z)" {
		t.Fatalf("Summary() = %q", got)
	}

	version, commit, builtAt = " ", "", "\t"
	info = Current()
	if info.Version != "2.0.0-dev" || info.Commit != "unknown" || info.BuiltAt != "unknown" {
		t.Fatalf("fallback Current() = %+v", info)
	}
}
