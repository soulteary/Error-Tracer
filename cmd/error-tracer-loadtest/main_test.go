package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunLoadCountsTruncatedResponsesAsTransportErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", "100")
		response.WriteHeader(http.StatusAccepted)
		_, _ = response.Write([]byte("{}"))
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	summary, err := runLoad(ctx, testLoadConfig(server.URL))
	if err != nil {
		t.Fatalf("run load: %v", err)
	}
	if summary.transportErrors != 1 || summary.accepted != 0 {
		t.Fatalf("summary = %#v, want one transport error and no accepted responses", summary)
	}
}

func TestRunLoadMeasuresResponseBodyCompletion(t *testing.T) {
	const bodyDelay = 60 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
		response.(http.Flusher).Flush()
		time.Sleep(bodyDelay)
		_, _ = response.Write([]byte("{}"))
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	summary, err := runLoad(ctx, testLoadConfig(server.URL))
	if err != nil {
		t.Fatalf("run load: %v", err)
	}
	if summary.accepted != 1 {
		t.Fatalf("accepted = %d, want 1", summary.accepted)
	}
	if summary.latency.max < bodyDelay {
		t.Fatalf("maximum latency = %s, want at least %s", summary.latency.max, bodyDelay)
	}
}

func testLoadConfig(endpoint string) loadConfig {
	return loadConfig{
		endpoint:       endpoint,
		projectKey:     "0123456789abcdef",
		timeout:        time.Second,
		concurrency:    1,
		batchSize:      1,
		cardinality:    1,
		requestsPerSec: 1,
	}
}

func TestParseConfigUsesEnvironmentKeyAndNormalizesTarget(t *testing.T) {
	configuration, err := parseConfig(
		[]string{
			"-target", "http://127.0.0.1:8080",
			"-duration", "2s",
			"-concurrency", "4",
			"-batch-size", "20",
			"-cardinality", "40",
			"-rate", "100",
		},
		func(name string) string {
			if name == ingestKeyEnvironment {
				return "0123456789abcdef"
			}
			return ""
		},
		io.Discard,
	)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if configuration.endpoint != "http://127.0.0.1:8080/api/v1/events/batch" {
		t.Fatalf("endpoint = %q", configuration.endpoint)
	}
	if configuration.projectKey != "0123456789abcdef" ||
		configuration.duration != 2*time.Second ||
		configuration.concurrency != 4 ||
		configuration.batchSize != 20 ||
		configuration.cardinality != 40 ||
		configuration.requestsPerSec != 100 {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestParseConfigProtectsRemoteTargets(t *testing.T) {
	base := []string{
		"-target", "https://errors.example.com",
		"-project-key", "0123456789abcdef",
	}
	if _, err := parseConfig(base, func(string) string { return "" }, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "-allow-remote") {
		t.Fatalf("remote target error = %v", err)
	}

	configuration, err := parseConfig(
		append(base, "-allow-remote"), func(string) string { return "" }, io.Discard,
	)
	if err != nil {
		t.Fatalf("allow remote target: %v", err)
	}
	if configuration.endpoint != "https://errors.example.com/api/v1/events/batch" {
		t.Fatalf("endpoint = %q", configuration.endpoint)
	}
}

func TestNormalizeTargetRejectsAmbiguousURLs(t *testing.T) {
	for _, target := range []string{
		"ftp://127.0.0.1/file",
		"http://user@127.0.0.1:8080",
		"http://127.0.0.1:8080/api/v1/events",
		"http://127.0.0.1:8080?query=1",
		"http://127.0.0.1:8080#fragment",
	} {
		if _, err := normalizeTarget(target, false); err == nil {
			t.Fatalf("normalizeTarget(%q) error = nil", target)
		}
	}
}

func TestBuildPayloadsControlsBatchSizeAndCardinality(t *testing.T) {
	configuration := loadConfig{
		projectKey:  "0123456789abcdef",
		batchSize:   3,
		cardinality: 5,
	}
	payloads, err := buildPayloads(configuration)
	if err != nil {
		t.Fatalf("build payloads: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("payload count = %d, want 2", len(payloads))
	}
	messages := make(map[string]bool)
	for _, encoded := range payloads {
		var payload loadPayload
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.ProjectKey != configuration.projectKey || len(payload.Events) != 3 {
			t.Fatalf("payload = %#v", payload)
		}
		for _, captured := range payload.Events {
			messages[captured.Message] = true
		}
	}
	if len(messages) != 5 {
		t.Fatalf("distinct messages = %d, want 5", len(messages))
	}
}

func TestHistogramReportsBoundedPercentiles(t *testing.T) {
	histogram := newHistogram()
	for _, value := range []time.Duration{
		100 * time.Microsecond,
		800 * time.Microsecond,
		3 * time.Millisecond,
		12 * time.Millisecond,
		90 * time.Second,
	} {
		histogram.observe(value)
	}
	if got := histogram.percentile(0.50); got != 5*time.Millisecond {
		t.Fatalf("p50 = %s, want %s", got, 5*time.Millisecond)
	}
	if got := histogram.percentile(0.99); got != 90*time.Second {
		t.Fatalf("p99 = %s, want %s", got, 90*time.Second)
	}
}

func TestPrintSummaryDoesNotExposeProjectKey(t *testing.T) {
	configuration := loadConfig{
		endpoint:    "http://127.0.0.1:8080/api/v1/events/batch",
		projectKey:  "secret-project-key",
		concurrency: 2,
		batchSize:   10,
	}
	histogram := newHistogram()
	histogram.observe(time.Millisecond)
	summary := loadSummary{
		elapsed:   time.Second,
		requests:  1,
		accepted:  1,
		statuses:  map[int]uint64{202: 1},
		latency:   histogram,
		batchSize: 10,
	}
	var output bytes.Buffer
	printSummary(&output, configuration, summary)
	if strings.Contains(output.String(), configuration.projectKey) {
		t.Fatal("summary exposed the project key")
	}
}
