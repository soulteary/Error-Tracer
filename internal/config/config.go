package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

const (
	defaultAddress         = ":8080"
	defaultDatabasePath    = "error-tracer.db"
	defaultShutdownTimeout = 10 * time.Second
)

// Config contains process-level settings for the Error-Tracer service.
type Config struct {
	Address         string
	DatabasePath    string
	ShutdownTimeout time.Duration
	ProjectID       string
	IngestKey       string
}

// FromEnvironment loads configuration without mutating process state.
func FromEnvironment() (Config, error) {
	address := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADDRESS"))
	if address == "" {
		address = defaultAddress
	}
	databasePath := strings.TrimSpace(os.Getenv("ERROR_TRACER_DATABASE_PATH"))
	if databasePath == "" {
		databasePath = defaultDatabasePath
	}
	projectID := strings.TrimSpace(os.Getenv("ERROR_TRACER_PROJECT_ID"))
	if projectID == "" {
		projectID = "default"
	}
	ingestKey := strings.TrimSpace(os.Getenv("ERROR_TRACER_INGEST_KEY"))
	if len(ingestKey) < 16 {
		return Config{}, errors.New("ERROR_TRACER_INGEST_KEY must contain at least 16 characters")
	}

	return Config{
		Address:         address,
		DatabasePath:    databasePath,
		ShutdownTimeout: defaultShutdownTimeout,
		ProjectID:       projectID,
		IngestKey:       ingestKey,
	}, nil
}
