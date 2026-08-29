package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress         = ":8080"
	defaultDatabasePath    = "error-tracer.db"
	defaultRatePerMinute   = 120
	defaultRateBurst       = 30
	defaultShutdownTimeout = 10 * time.Second
)

// Config contains process-level settings for the Error-Tracer service.
type Config struct {
	Address         string
	DatabasePath    string
	ShutdownTimeout time.Duration
	ProjectID       string
	IngestKey       string
	AdminToken      string
	AllowedOrigins  []string
	RatePerMinute   int
	RateBurst       int
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
	adminToken := strings.TrimSpace(os.Getenv("ERROR_TRACER_ADMIN_TOKEN"))
	if len(adminToken) < 24 {
		return Config{}, errors.New("ERROR_TRACER_ADMIN_TOKEN must contain at least 24 characters")
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

	return Config{
		Address:         address,
		DatabasePath:    databasePath,
		ShutdownTimeout: defaultShutdownTimeout,
		ProjectID:       projectID,
		IngestKey:       ingestKey,
		AdminToken:      adminToken,
		AllowedOrigins:  allowedOrigins,
		RatePerMinute:   ratePerMinute,
		RateBurst:       rateBurst,
	}, nil
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
		origin := scheme + "://" + strings.ToLower(parsed.Host)
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}
