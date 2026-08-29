package config

import (
	"reflect"
	"testing"
	"time"
)

func TestFromEnvironmentUsesDefaults(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", "")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", "")
	t.Setenv("ERROR_TRACER_PROJECT_ID", "")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "development-key-1")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "development-admin-token-1")
	t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", "")

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
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", " 0123456789abcdefghijklmn ")
	t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", " HTTPS://APP.EXAMPLE.COM/ ,https://admin.example.com,https://app.example.com ")

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
	if cfg.AdminToken != "0123456789abcdefghijklmn" {
		t.Fatalf("AdminToken was not loaded")
	}
	wantOrigins := []string{"https://app.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, wantOrigins) {
		t.Fatalf("AllowedOrigins = %#v, want %#v", cfg.AllowedOrigins, wantOrigins)
	}
}

func TestFromEnvironmentRequiresIngestKey(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "short")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")

	if _, err := FromEnvironment(); err == nil {
		t.Fatal("FromEnvironment() error = nil, want invalid ingest key error")
	}
}

func TestFromEnvironmentRequiresAdminToken(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "short")

	if _, err := FromEnvironment(); err == nil {
		t.Fatal("FromEnvironment() error = nil, want invalid admin token error")
	}
}

func TestFromEnvironmentRejectsInvalidOrigins(t *testing.T) {
	tests := []string{
		"*",
		"app.example.com",
		"ftp://app.example.com",
		"https://user@app.example.com",
		"https://app.example.com/path",
		"https://app.example.com?query=1",
		"https://app.example.com#fragment",
		"https://app.example.com,",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", value)

			if _, err := FromEnvironment(); err == nil {
				t.Fatalf("FromEnvironment() error = nil for %q", value)
			}
		})
	}
}
