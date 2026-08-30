// Package buildinfo exposes release metadata injected by the build pipeline.
package buildinfo

import (
	"fmt"
	"strings"
)

var (
	version = "2.0.0-dev"
	commit  = "unknown"
	builtAt = "unknown"
)

// Info describes one Error-Tracer build.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

// Current returns immutable, normalized build metadata.
func Current() Info {
	return Info{
		Version: normalized(version, "2.0.0-dev"),
		Commit:  normalized(commit, "unknown"),
		BuiltAt: normalized(builtAt, "unknown"),
	}
}

// Summary returns a compact human-readable version string.
func Summary() string {
	info := Current()
	return fmt.Sprintf(
		"error-tracer %s (commit %s, built %s)",
		info.Version, info.Commit, info.BuiltAt,
	)
}

func normalized(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
