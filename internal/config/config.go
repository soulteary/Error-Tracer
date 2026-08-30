package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress           = ":8080"
	defaultDatabasePath      = "error-tracer.db"
	defaultSQLiteConnections = 4
	defaultMaxEventsPerIssue = 100
	defaultRatePerMinute     = 120
	defaultRateBurst         = 30
	defaultShutdownTimeout   = 10 * time.Second
	maxRetentionDays         = 3650
)

// Config contains process-level settings for the Error-Tracer service.
type Config struct {
	Address                  string
	DatabasePath             string
	SQLiteMaxOpenConnections int
	MaxEventsPerIssue        int
	ShutdownTimeout          time.Duration
	ProjectID                string
	IngestKey                string
	AdminToken               string
	PreviousAdminToken       string
	AllowedOrigins           []string
	RatePerMinute            int
	RateBurst                int
	MetricsEnabled           bool
	DemoMode                 bool
	RetentionDays            int
}

// FromEnvironment loads configuration without mutating process state.
func FromEnvironment() (Config, error) {
	address := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADDRESS"))
	if address == "" {
		address = defaultAddress
	}
	databasePath := DatabasePathFromEnvironment()
	sqliteConnections, err := positiveInteger(
		"ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS", defaultSQLiteConnections, 32,
	)
	if err != nil {
		return Config{}, err
	}
	maxEventsPerIssue, err := positiveInteger(
		"ERROR_TRACER_MAX_EVENTS_PER_ISSUE", defaultMaxEventsPerIssue, 1000,
	)
	if err != nil {
		return Config{}, err
	}
	projectID := strings.TrimSpace(os.Getenv("ERROR_TRACER_PROJECT_ID"))
	if projectID == "" {
		projectID = "default"
	}
	ingestKey := strings.TrimSpace(os.Getenv("ERROR_TRACER_INGEST_KEY"))
	if len(ingestKey) < 16 {
		return Config{}, errors.New("ERROR_TRACER_INGEST_KEY must contain at least 16 bytes")
	}
	adminToken := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADMIN_TOKEN"))
	if len(adminToken) < 24 {
		return Config{}, errors.New("ERROR_TRACER_ADMIN_TOKEN must contain at least 24 bytes")
	}
	previousAdminToken := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS"))
	if previousAdminToken != "" && len(previousAdminToken) < 24 {
		return Config{}, errors.New("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS must be empty or contain at least 24 bytes")
	}
	if previousAdminToken != "" && previousAdminToken == adminToken {
		return Config{}, errors.New("ERROR_TRACER_ADMIN_TOKEN_PREVIOUS must differ from ERROR_TRACER_ADMIN_TOKEN")
	}
	allowedOrigins, err := parseOrigins(os.Getenv("ERROR_TRACER_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	ratePerMinute, err := positiveInteger("ERROR_TRACER_RATE_PER_MINUTE", defaultRatePerMinute, 60_000)
	if err != nil {
		return Config{}, err
	}
	rateBurst, err := positiveInteger("ERROR_TRACER_RATE_BURST", defaultRateBurst, 10_000)
	if err != nil {
		return Config{}, err
	}
	metricsEnabled, err := strictBoolean("ERROR_TRACER_METRICS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	demoMode, err := strictBoolean("ERROR_TRACER_DEMO_MODE", false)
	if err != nil {
		return Config{}, err
	}
	retentionDays, err := nonNegativeInteger("ERROR_TRACER_RETENTION_DAYS", 0, maxRetentionDays)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Address:                  address,
		DatabasePath:             databasePath,
		SQLiteMaxOpenConnections: sqliteConnections,
		MaxEventsPerIssue:        maxEventsPerIssue,
		ShutdownTimeout:          defaultShutdownTimeout,
		ProjectID:                projectID,
		IngestKey:                ingestKey,
		AdminToken:               adminToken,
		PreviousAdminToken:       previousAdminToken,
		AllowedOrigins:           allowedOrigins,
		RatePerMinute:            ratePerMinute,
		RateBurst:                rateBurst,
		MetricsEnabled:           metricsEnabled,
		DemoMode:                 demoMode,
		RetentionDays:            retentionDays,
	}, nil
}

// DatabasePathFromEnvironment returns the configured SQLite path without
// requiring service credentials. Maintenance commands therefore do not need
// ingestion or administration secrets.
func DatabasePathFromEnvironment() string {
	databasePath := strings.TrimSpace(os.Getenv("ERROR_TRACER_DATABASE_PATH"))
	if databasePath == "" {
		return defaultDatabasePath
	}
	return databasePath
}

func strictBoolean(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	switch strings.ToLower(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func positiveInteger(name string, fallback, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", name, maximum)
	}
	return value, nil
}

func nonNegativeInteger(name string, fallback, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between 0 and %d", name, maximum)
	}
	return value, nil
}

func parseOrigins(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	origins := make([]string, 0, strings.Count(value, ",")+1)
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("ERROR_TRACER_ALLOWED_ORIGINS contains invalid origin %q", raw)
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			return nil, fmt.Errorf("ERROR_TRACER_ALLOWED_ORIGINS origin %q must use http or https", raw)
		}
		if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("ERROR_TRACER_ALLOWED_ORIGINS value %q must not contain credentials, a path, query, or fragment", raw)
		}
		hostname := strings.ToLower(parsed.Hostname())
		port := parsed.Port()
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			port = ""
		}
		host := hostname
		if port != "" {
			host = net.JoinHostPort(hostname, port)
		} else if strings.Contains(hostname, ":") {
			host = "[" + hostname + "]"
		}
		origin := scheme + "://" + host
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}
