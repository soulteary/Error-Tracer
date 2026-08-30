package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFromEnvironmentUsesDefaults(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", "")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", "")
	t.Setenv("ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS", "")
	t.Setenv("ERROR_TRACER_MAX_EVENTS_PER_ISSUE", "")
	t.Setenv("ERROR_TRACER_PROJECT_ID", "")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "development-key-1")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "development-admin-token-1")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS", "")
	t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", "")
	t.Setenv("ERROR_TRACER_METRICS_ENABLED", "")
	t.Setenv("ERROR_TRACER_RATE_PER_MINUTE", "")
	t.Setenv("ERROR_TRACER_RATE_BURST", "")
	t.Setenv("ERROR_TRACER_DEMO_MODE", "")
	t.Setenv("ERROR_TRACER_RETENTION_DAYS", "")

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
	if cfg.SQLiteMaxOpenConnections != 4 {
		t.Fatalf("SQLiteMaxOpenConnections = %d, want 4", cfg.SQLiteMaxOpenConnections)
	}
	if cfg.MaxEventsPerIssue != 100 {
		t.Fatalf("MaxEventsPerIssue = %d, want 100", cfg.MaxEventsPerIssue)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, 10*time.Second)
	}
	if cfg.ProjectID != "default" {
		t.Fatalf("ProjectID = %q, want %q", cfg.ProjectID, "default")
	}
	if cfg.RatePerMinute != 120 || cfg.RateBurst != 30 {
		t.Fatalf("rate = %d/minute burst %d, want 120/minute burst 30", cfg.RatePerMinute, cfg.RateBurst)
	}
	if cfg.MetricsEnabled {
		t.Fatal("MetricsEnabled = true, want false")
	}
	if cfg.DemoMode {
		t.Fatal("DemoMode = true, want false")
	}
	if cfg.RetentionDays != 0 {
		t.Fatalf("RetentionDays = %d, want 0", cfg.RetentionDays)
	}
}

func TestDatabasePathFromEnvironmentDoesNotRequireCredentials(t *testing.T) {
	t.Setenv("ERROR_TRACER_DATABASE_PATH", " /var/lib/error-tracer/maintenance.db ")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "")
	if got := DatabasePathFromEnvironment(); got != "/var/lib/error-tracer/maintenance.db" {
		t.Fatalf("DatabasePathFromEnvironment() = %q", got)
	}
	t.Setenv("ERROR_TRACER_DATABASE_PATH", "")
	if got := DatabasePathFromEnvironment(); got != "error-tracer.db" {
		t.Fatalf("default DatabasePathFromEnvironment() = %q", got)
	}
}

func TestFromEnvironmentReadsAddress(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", " 127.0.0.1:9090 ")
	t.Setenv("ERROR_TRACER_DATABASE_PATH", " /var/lib/error-tracer/events.db ")
	t.Setenv("ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS", " 8 ")
	t.Setenv("ERROR_TRACER_MAX_EVENTS_PER_ISSUE", " 250 ")
	t.Setenv("ERROR_TRACER_PROJECT_ID", " project-a ")
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", " 0123456789abcdefghijklmn ")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS", " previous-admin-token-1234 ")
	t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", " HTTPS://APP.EXAMPLE.COM/ ,https://admin.example.com,https://app.example.com ")
	t.Setenv("ERROR_TRACER_METRICS_ENABLED", " true ")
	t.Setenv("ERROR_TRACER_RATE_PER_MINUTE", " 240 ")
	t.Setenv("ERROR_TRACER_RATE_BURST", " 40 ")
	t.Setenv("ERROR_TRACER_DEMO_MODE", " true ")
	t.Setenv("ERROR_TRACER_RETENTION_DAYS", " 90 ")

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
	if cfg.SQLiteMaxOpenConnections != 8 {
		t.Fatalf("SQLiteMaxOpenConnections = %d, want 8", cfg.SQLiteMaxOpenConnections)
	}
	if cfg.MaxEventsPerIssue != 250 {
		t.Fatalf("MaxEventsPerIssue = %d, want 250", cfg.MaxEventsPerIssue)
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
	if cfg.PreviousAdminToken != "previous-admin-token-1234" {
		t.Fatalf("PreviousAdminToken was not loaded")
	}
	wantOrigins := []string{"https://app.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, wantOrigins) {
		t.Fatalf("AllowedOrigins = %#v, want %#v", cfg.AllowedOrigins, wantOrigins)
	}
	if cfg.RatePerMinute != 240 || cfg.RateBurst != 40 {
		t.Fatalf("rate = %d/minute burst %d, want 240/minute burst 40", cfg.RatePerMinute, cfg.RateBurst)
	}
	if !cfg.MetricsEnabled {
		t.Fatal("MetricsEnabled = false, want true")
	}
	if !cfg.DemoMode {
		t.Fatal("DemoMode = false, want true")
	}
	if cfg.RetentionDays != 90 {
		t.Fatalf("RetentionDays = %d, want 90", cfg.RetentionDays)
	}
}

func TestFromEnvironmentRejectsInvalidRetentionDays(t *testing.T) {
	for _, value := range []string{"-1", "many", "3651"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			t.Setenv("ERROR_TRACER_RETENTION_DAYS", value)
			if _, err := FromEnvironment(); err == nil {
				t.Fatalf("FromEnvironment() error = nil for retention %q", value)
			}
		})
	}
}

func TestFromEnvironmentRejectsInvalidDemoMode(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
	t.Setenv("ERROR_TRACER_DEMO_MODE", "1")

	_, err := FromEnvironment()
	if err == nil || err.Error() != "ERROR_TRACER_DEMO_MODE must be true or false" {
		t.Fatalf("FromEnvironment() error = %v, want strict demo mode error", err)
	}
}

func TestFromEnvironmentRejectsInvalidMetricsMode(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
	t.Setenv("ERROR_TRACER_METRICS_ENABLED", "1")

	_, err := FromEnvironment()
	if err == nil || err.Error() != "ERROR_TRACER_METRICS_ENABLED must be true or false" {
		t.Fatalf("FromEnvironment() error = %v, want strict metrics mode error", err)
	}
}

func TestFromEnvironmentRequiresIngestKey(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "short")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")

	_, err := FromEnvironment()
	if err == nil || err.Error() != "ERROR_TRACER_INGEST_KEY must contain at least 16 bytes" {
		t.Fatalf("FromEnvironment() error = %v, want ingest key byte-length error", err)
	}
}

func TestFromEnvironmentRequiresAdminToken(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "short")

	_, err := FromEnvironment()
	if err == nil || err.Error() != "ERROR_TRACER_ADMIN_TOKEN must contain at least 24 bytes" {
		t.Fatalf("FromEnvironment() error = %v, want admin token byte-length error", err)
	}
}

func TestFromEnvironmentValidatesPreviousAdminToken(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		want     string
	}{
		{
			name:     "too short",
			previous: "short",
			want:     "ERROR_TRACER_ADMIN_TOKEN_PREVIOUS must be empty or contain at least 24 bytes",
		},
		{
			name:     "same as current",
			previous: "0123456789abcdefghijklmn",
			want:     "ERROR_TRACER_ADMIN_TOKEN_PREVIOUS must differ from ERROR_TRACER_ADMIN_TOKEN",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS", test.previous)
			_, err := FromEnvironment()
			if err == nil || err.Error() != test.want {
				t.Fatalf("FromEnvironment() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFromEnvironmentCountsCredentialBytes(t *testing.T) {
	t.Setenv("ERROR_TRACER_INGEST_KEY", strings.Repeat("é", 8))
	t.Setenv("ERROR_TRACER_ADMIN_TOKEN", strings.Repeat("é", 12))

	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment() error = %v", err)
	}
	if len(cfg.IngestKey) != 16 || len(cfg.AdminToken) != 24 {
		t.Fatalf("credential lengths = %d and %d bytes, want 16 and 24", len(cfg.IngestKey), len(cfg.AdminToken))
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

func TestParseOriginsRemovesDefaultPorts(t *testing.T) {
	origins, err := parseOrigins(strings.Join([]string{
		"https://app.example.com:443",
		"https://app.example.com",
		"http://localhost:80",
		"http://localhost",
		"https://app.example.com:8443",
		"http://[::1]:80",
	}, ","))
	if err != nil {
		t.Fatalf("parseOrigins() error = %v", err)
	}
	want := []string{
		"https://app.example.com",
		"http://localhost",
		"https://app.example.com:8443",
		"http://[::1]",
	}
	if !reflect.DeepEqual(origins, want) {
		t.Fatalf("parseOrigins() = %#v, want %#v", origins, want)
	}
}

func TestFromEnvironmentRejectsInvalidRateLimits(t *testing.T) {
	tests := []struct {
		name     string
		variable string
		value    string
	}{
		{name: "rate zero", variable: "ERROR_TRACER_RATE_PER_MINUTE", value: "0"},
		{name: "rate negative", variable: "ERROR_TRACER_RATE_PER_MINUTE", value: "-1"},
		{name: "rate text", variable: "ERROR_TRACER_RATE_PER_MINUTE", value: "many"},
		{name: "rate too large", variable: "ERROR_TRACER_RATE_PER_MINUTE", value: "60001"},
		{name: "burst zero", variable: "ERROR_TRACER_RATE_BURST", value: "0"},
		{name: "burst negative", variable: "ERROR_TRACER_RATE_BURST", value: "-1"},
		{name: "burst too large", variable: "ERROR_TRACER_RATE_BURST", value: "10001"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			t.Setenv("ERROR_TRACER_ALLOWED_ORIGINS", "")
			t.Setenv("ERROR_TRACER_RATE_PER_MINUTE", "")
			t.Setenv("ERROR_TRACER_RATE_BURST", "")
			t.Setenv(test.variable, test.value)

			if _, err := FromEnvironment(); err == nil {
				t.Fatalf("FromEnvironment() error = nil for %s=%q", test.variable, test.value)
			}
		})
	}
}

func TestFromEnvironmentRejectsInvalidSQLiteConnectionCount(t *testing.T) {
	for _, value := range []string{"0", "-1", "many", "33"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS", value)
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			if _, err := FromEnvironment(); err == nil {
				t.Fatalf("FromEnvironment() error = nil for SQLite connections %q", value)
			}
		})
	}
}

func TestFromEnvironmentRejectsInvalidEventHistoryLimit(t *testing.T) {
	for _, value := range []string{"0", "-1", "many", "1001"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ERROR_TRACER_MAX_EVENTS_PER_ISSUE", value)
			t.Setenv("ERROR_TRACER_INGEST_KEY", "0123456789abcdef")
			t.Setenv("ERROR_TRACER_ADMIN_TOKEN", "0123456789abcdefghijklmn")
			if _, err := FromEnvironment(); err == nil {
				t.Fatalf("FromEnvironment() error = nil for event history limit %q", value)
			}
		})
	}
}
