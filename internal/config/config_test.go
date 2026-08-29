package config

import (
	"testing"
	"time"
)

func TestFromEnvironmentUsesDefaults(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", "")

	cfg := FromEnvironment()
	if cfg.Address != ":8080" {
		t.Fatalf("Address = %q, want %q", cfg.Address, ":8080")
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, 10*time.Second)
	}
}

func TestFromEnvironmentReadsAddress(t *testing.T) {
	t.Setenv("ERROR_TRACER_ADDRESS", " 127.0.0.1:9090 ")

	cfg := FromEnvironment()
	if cfg.Address != "127.0.0.1:9090" {
		t.Fatalf("Address = %q, want %q", cfg.Address, "127.0.0.1:9090")
	}
}
