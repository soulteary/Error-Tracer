package config

import (
	"testing"
	"time"
)

func TestFromEnvironmentUsesDefaults(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", "")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", "")
	t.Setenv("ERROR_TRACER_PROJECT_ID", "")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "development-key-1")

	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment() error = %v", err)
	}
	if cfg.Address != ":8080" {
		t.Fatalf("Address = %q, want %q", cfg.Address, ":8080")
	}
	if cfg.DatabasePath != "error-tracer.db" {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, "error-tracer.db")
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, 10*time.Second)
	}
	if cfg.ProjectID != "default" {
		t.Fatalf("ProjectID = %q, want %q", cfg.ProjectID, "default")
	}
}

func TestFromEnvironmentReadsAddress(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", " 127.0.0.1:9090 ")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", " /var/lib/error-tracer/events.db ")
	t.Setenv("ERROR_TRACER_PROJECT_ID", " project-a ")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")

	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment() error = %v", err)
	}
	if cfg.Address != "127.0.0.1:9090" {
		t.Fatalf("Address = %q, want %q", cfg.Address, "127.0.0.1:9090")
	}
	if cfg.DatabasePath != "/var/lib/error-tracer/events.db" {
		t.Fatalf("DatabasePath = %q, want trimmed path", cfg.DatabasePath)
	}
	if cfg.ProjectID != "project-a" {
		t.Fatalf("ProjectID = %q, want %q", cfg.ProjectID, "project-a")
	}
	if cfg.IngestKey != "0123456789abcdef" {
		t.Fatalf("IngestKey was not loaded")
	}
}

func TestFromEnvironmentRequiresIngestKey(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "short")

	if _, err := FromEnvironment(); err == nil {
		t.Fatal("FromEnvironment() error = nil, want invalid ingest key error")
	}
}
