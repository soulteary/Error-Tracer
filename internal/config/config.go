package config

import (
	"os"
	"strings"
	"time"
)

const (
	defaultAddress         = ":8080"
	defaultShutdownTimeout = 10 * time.Second
)

// Config contains process-level settings for the Error-Tracer service.
type Config struct {
	Address         string
	ShutdownTimeout time.Duration
}

// FromEnvironment loads configuration without mutating process state.
func FromEnvironment() Config {
	address := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADDRESS"))
	if address == "" {
		address = defaultAddress
	}

	return Config{
		Address:         address,
		ShutdownTimeout: defaultShutdownTimeout,
	}
}
