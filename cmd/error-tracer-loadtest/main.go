// Command error-tracer-loadtest drives the batch ingestion endpoint for
// deliberate performance and capacity testing.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	ingestKeyEnvironment = "ERROR_TRACER_INGEST_KEY"
	batchEndpointPath    = "/api/v1/events/batch"
)

var latencyBounds = []time.Duration{
	250 * time.Microsecond,
	500 * time.Microsecond,
	1 * time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

type loadConfig struct {
	endpoint       string
	projectKey     string
	duration       time.Duration
	timeout        time.Duration
	concurrency    int
	batchSize      int
	cardinality    int
	requestsPerSec int
	failOnError    bool
}

type loadEvent struct {
	Kind        string            `json:"kind"`
	Message     string            `json:"message"`
	SourceURL   string            `json:"source_url"`
	PageURL     string            `json:"page_url"`
	Line        int               `json:"line"`
	Column      int               `json:"column"`
	Environment string            `json:"environment"`
	Tags        map[string]string `json:"tags"`
}

type loadPayload struct {
	ProjectKey string      `json:"project_key"`
	Events     []loadEvent `json:"events"`
}

type observation struct {
	status    int
	latency   time.Duration
	transport bool
}

type histogram struct {
	counts []uint64
	total  uint64
	max    time.Duration
}

type loadSummary struct {
	elapsed         time.Duration
	requests        uint64
	accepted        uint64
	transportErrors uint64
	statuses        map[int]uint64
	latency         histogram
	batchSize       int
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	configuration, err := parseConfig(args, getenv, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	loadContext, cancel := context.WithTimeout(signalContext, configuration.duration)
	defer cancel()

	summary, err := runLoad(loadContext, configuration)
	if err != nil {
		fmt.Fprintf(stderr, "load test error: %v\n", err)
		return 2
	}
	printSummary(stdout, configuration, summary)
	if configuration.failOnError && summary.failed() {
		return 1
	}
	return 0
}

func parseConfig(args []string, getenv func(string) string, output io.Writer) (loadConfig, error) {
	flags := flag.NewFlagSet("error-tracer-loadtest", flag.ContinueOnError)
	flags.SetOutput(output)
	target := flags.String("target", "", "base URL or complete batch endpoint (required)")
	projectKey := flags.String("project-key", "", "ingest key; defaults to ERROR_TRACER_INGEST_KEY")
	duration := flags.Duration("duration", 10*time.Second, "test duration (1s to 30m)")
	timeout := flags.Duration("timeout", 5*time.Second, "per-request timeout (100ms to 1m)")
	concurrency := flags.Int("concurrency", 8, "number of concurrent workers (1 to 1000)")
	batchSize := flags.Int("batch-size", 10, "events per request (1 to 100)")
	cardinality := flags.Int("cardinality", 100, "distinct issue fingerprints (1 to 100000)")
	requestsPerSec := flags.Int("rate", 0, "aggregate requests per second; 0 means unlimited")
	allowRemote := flags.Bool("allow-remote", false, "permit a non-loopback target")
	failOnError := flags.Bool("fail-on-error", true, "exit non-zero for transport or non-202 responses")
	if err := flags.Parse(args); err != nil {
		return loadConfig{}, err
	}
	if flags.NArg() != 0 {
		return loadConfig{}, errors.New("positional arguments are not supported")
	}

	endpoint, err := normalizeTarget(*target, *allowRemote)
	if err != nil {
		return loadConfig{}, err
	}
	key := strings.TrimSpace(*projectKey)
	if key == "" {
		key = strings.TrimSpace(getenv(ingestKeyEnvironment))
	}
	if len(key) < 16 {
		return loadConfig{}, errors.New("project key must contain at least 16 bytes")
	}
	if *duration < time.Second || *duration > 30*time.Minute {
		return loadConfig{}, errors.New("duration must be between 1s and 30m")
	}
	if *timeout < 100*time.Millisecond || *timeout > time.Minute {
		return loadConfig{}, errors.New("timeout must be between 100ms and 1m")
	}
	if *concurrency < 1 || *concurrency > 1_000 {
		return loadConfig{}, errors.New("concurrency must be between 1 and 1000")
	}
	if *batchSize < 1 || *batchSize > 100 {
		return loadConfig{}, errors.New("batch size must be between 1 and 100")
	}
	if *cardinality < 1 || *cardinality > 100_000 {
		return loadConfig{}, errors.New("cardinality must be between 1 and 100000")
	}
	if *requestsPerSec < 0 || *requestsPerSec > 1_000_000 {
		return loadConfig{}, errors.New("rate must be between 0 and 1000000 requests per second")
	}

	return loadConfig{
		endpoint:       endpoint,
		projectKey:     key,
		duration:       *duration,
		timeout:        *timeout,
		concurrency:    *concurrency,
		batchSize:      *batchSize,
		cardinality:    *cardinality,
		requestsPerSec: *requestsPerSec,
		failOnError:    *failOnError,
	}, nil
}

func normalizeTarget(value string, allowRemote bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("target is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("target must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("target must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("target must not contain credentials, a query, or a fragment")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = batchEndpointPath
	} else if parsed.Path != batchEndpointPath {
		return "", fmt.Errorf("target path must be empty or %s", batchEndpointPath)
	}
	if !allowRemote && !loopbackHost(parsed.Hostname()) {
		return "", errors.New("refusing a non-loopback target without -allow-remote")
	}
	return parsed.String(), nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func runLoad(ctx context.Context, configuration loadConfig) (loadSummary, error) {
	payloads, err := buildPayloads(configuration)
	if err != nil {
		return loadSummary{}, err
	}

	transport := &http.Transport{
		MaxIdleConns:        configuration.concurrency * 2,
		MaxIdleConnsPerHost: configuration.concurrency,
		MaxConnsPerHost:     configuration.concurrency,
		IdleConnTimeout:     30 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   configuration.timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var ticker *time.Ticker
	var permits <-chan time.Time
	var initialPermit atomic.Bool
	if configuration.requestsPerSec > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(configuration.requestsPerSec))
		defer ticker.Stop()
		permits = ticker.C
		initialPermit.Store(true)
	}

	started := time.Now()
	results := make(chan observation, configuration.concurrency*2)
	var sequence atomic.Uint64
	var workers sync.WaitGroup
	for range configuration.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for waitForPermit(ctx, permits, &initialPermit) {
				index := sequence.Add(1) - 1
				payload := payloads[index%uint64(len(payloads))]
				request, requestErr := http.NewRequestWithContext(
					ctx, http.MethodPost, configuration.endpoint, bytes.NewReader(payload),
				)
				if requestErr != nil {
					results <- observation{transport: true}
					continue
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json")
				request.Header.Set("User-Agent", "error-tracer-loadtest/1")

				requestStarted := time.Now()
				response, requestErr := client.Do(request)
				latency := time.Since(requestStarted)
				if requestErr != nil {
					if ctx.Err() != nil {
						return
					}
					results <- observation{latency: latency, transport: true}
					continue
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
				_ = response.Body.Close()
				results <- observation{status: response.StatusCode, latency: latency}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	summary := loadSummary{
		statuses:  make(map[int]uint64),
		latency:   newHistogram(),
		batchSize: configuration.batchSize,
	}
	for result := range results {
		summary.requests++
		if result.transport {
			summary.transportErrors++
			continue
		}
		summary.statuses[result.status]++
		summary.latency.observe(result.latency)
		if result.status == http.StatusAccepted {
			summary.accepted++
		}
	}
	summary.elapsed = time.Since(started)
	return summary, nil
}

func waitForPermit(ctx context.Context, permits <-chan time.Time, initial *atomic.Bool) bool {
	if permits == nil {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	if initial.CompareAndSwap(true, false) {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-permits:
		return true
	}
}

func buildPayloads(configuration loadConfig) ([][]byte, error) {
	payloadCount := max(1, (configuration.cardinality+configuration.batchSize-1)/configuration.batchSize)
	payloads := make([][]byte, payloadCount)
	for payloadIndex := range payloads {
		events := make([]loadEvent, configuration.batchSize)
		for eventIndex := range events {
			fingerprintIndex := (payloadIndex*configuration.batchSize + eventIndex) % configuration.cardinality
			events[eventIndex] = loadEvent{
				Kind:        "error",
				Message:     fmt.Sprintf("load test failure %06d", fingerprintIndex),
				SourceURL:   "https://loadtest.invalid/assets/app.js",
				PageURL:     "https://loadtest.invalid/checkout",
				Line:        42,
				Column:      7,
				Environment: "loadtest",
				Tags:        map[string]string{"suite": "pressure"},
			}
		}
		encoded, err := json.Marshal(loadPayload{
			ProjectKey: configuration.projectKey,
			Events:     events,
		})
		if err != nil {
			return nil, fmt.Errorf("encode payload %d: %w", payloadIndex, err)
		}
		payloads[payloadIndex] = encoded
	}
	return payloads, nil
}

func newHistogram() histogram {
	return histogram{counts: make([]uint64, len(latencyBounds)+1)}
}

func (h *histogram) observe(value time.Duration) {
	index := sort.Search(len(latencyBounds), func(index int) bool {
		return value <= latencyBounds[index]
	})
	h.counts[index]++
	h.total++
	if value > h.max {
		h.max = value
	}
}

func (h histogram) percentile(quantile float64) time.Duration {
	if h.total == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(h.total) * quantile))
	var cumulative uint64
	for index, count := range h.counts {
		cumulative += count
		if cumulative >= target {
			if index < len(latencyBounds) {
				return latencyBounds[index]
			}
			return h.max
		}
	}
	return h.max
}

func (summary loadSummary) failed() bool {
	if summary.accepted == 0 || summary.transportErrors != 0 {
		return true
	}
	for status, count := range summary.statuses {
		if status != http.StatusAccepted && count != 0 {
			return true
		}
	}
	return false
}

func printSummary(output io.Writer, configuration loadConfig, summary loadSummary) {
	seconds := max(summary.elapsed.Seconds(), 0.000_001)
	acceptedEvents := summary.accepted * uint64(summary.batchSize)
	fmt.Fprintln(output, "Error-Tracer load test")
	fmt.Fprintf(output, "Target:       %s\n", configuration.endpoint)
	fmt.Fprintf(output, "Elapsed:      %s\n", summary.elapsed.Round(time.Millisecond))
	fmt.Fprintf(output, "Concurrency:  %d\n", configuration.concurrency)
	fmt.Fprintf(output, "Batch size:   %d\n", configuration.batchSize)
	fmt.Fprintf(output, "Requests:     %d (%.1f req/s)\n", summary.requests, float64(summary.requests)/seconds)
	fmt.Fprintf(output, "Accepted:     %d requests / %d events (%.1f events/s)\n", summary.accepted, acceptedEvents, float64(acceptedEvents)/seconds)
	fmt.Fprintf(output, "Transport:    %d errors\n", summary.transportErrors)
	fmt.Fprintf(
		output, "Latency:      p50 %s / p95 %s / p99 %s / max %s\n",
		summary.latency.percentile(0.50), summary.latency.percentile(0.95),
		summary.latency.percentile(0.99), summary.latency.max,
	)

	statuses := make([]int, 0, len(summary.statuses))
	for status := range summary.statuses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	fmt.Fprint(output, "HTTP status:  ")
	if len(statuses) == 0 {
		fmt.Fprintln(output, "none")
		return
	}
	for index, status := range statuses {
		if index != 0 {
			fmt.Fprint(output, ", ")
		}
		fmt.Fprintf(output, "%d=%d", status, summary.statuses[status])
	}
	fmt.Fprintln(output)
}
