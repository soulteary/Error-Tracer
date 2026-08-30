package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

var requestDurationBuckets = [...]float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

type requestMetricKey struct {
	method string
	route  string
	status int
}

type durationMetricKey struct {
	method string
	route  string
}

type durationMetric struct {
	buckets [len(requestDurationBuckets) + 1]uint64
	count   uint64
	sum     float64
}

type serviceMetrics struct {
	mu        sync.Mutex
	requests  map[requestMetricKey]uint64
	durations map[durationMetricKey]durationMetric
	inFlight  atomic.Int64
	ingested  atomic.Uint64
}

func newServiceMetrics() *serviceMetrics {
	return &serviceMetrics{
		requests:  make(map[requestMetricKey]uint64),
		durations: make(map[durationMetricKey]durationMetric),
	}
}

func (m *serviceMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		// A scrape must not change the series it is reading.
		if request.URL.Path == "/metrics" {
			next.ServeHTTP(w, request)
			return
		}

		m.inFlight.Add(1)
		defer m.inFlight.Add(-1)
		started := time.Now()
		response := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(response, request)
		m.observe(request, response.status, time.Since(started).Seconds())
	})
}

func (m *serviceMetrics) observe(request *http.Request, status int, seconds float64) {
	method := metricMethod(request.Method)
	route := metricRoute(request.Pattern)
	requestKey := requestMetricKey{method: method, route: route, status: status}
	durationKey := durationMetricKey{method: method, route: route}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[requestKey]++
	duration := m.durations[durationKey]
	duration.count++
	duration.sum += seconds
	bucket := len(requestDurationBuckets)
	for index, upperBound := range requestDurationBuckets {
		if seconds <= upperBound {
			bucket = index
			break
		}
	}
	duration.buckets[bucket]++
	m.durations[durationKey] = duration
}

func (m *serviceMetrics) addIngested(count int) {
	if count > 0 {
		m.ingested.Add(uint64(count))
	}
}

func (s *Server) prometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", prometheusContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.metrics.render(
		s.effectiveReady(), s.storeReady.Load(), s.demoStore != nil,
	)))
}

func (m *serviceMetrics) render(ready, storeReady, demo bool) string {
	m.mu.Lock()
	requests := make(map[requestMetricKey]uint64, len(m.requests))
	for key, value := range m.requests {
		requests[key] = value
	}
	durations := make(map[durationMetricKey]durationMetric, len(m.durations))
	for key, value := range m.durations {
		durations[key] = value
	}
	m.mu.Unlock()

	requestKeys := make([]requestMetricKey, 0, len(requests))
	for key := range requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		if requestKeys[i].route != requestKeys[j].route {
			return requestKeys[i].route < requestKeys[j].route
		}
		if requestKeys[i].method != requestKeys[j].method {
			return requestKeys[i].method < requestKeys[j].method
		}
		return requestKeys[i].status < requestKeys[j].status
	})
	durationKeys := make([]durationMetricKey, 0, len(durations))
	for key := range durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		if durationKeys[i].route != durationKeys[j].route {
			return durationKeys[i].route < durationKeys[j].route
		}
		return durationKeys[i].method < durationKeys[j].method
	})

	var output strings.Builder
	output.WriteString("# HELP error_tracer_http_requests_total Completed HTTP requests.\n")
	output.WriteString("# TYPE error_tracer_http_requests_total counter\n")
	for _, key := range requestKeys {
		fmt.Fprintf(
			&output,
			"error_tracer_http_requests_total{method=%q,route=%q,status=%q} %d\n",
			metricLabel(key.method), metricLabel(key.route), strconv.Itoa(key.status), requests[key],
		)
	}
	output.WriteString("# HELP error_tracer_http_request_duration_seconds HTTP request duration.\n")
	output.WriteString("# TYPE error_tracer_http_request_duration_seconds histogram\n")
	for _, key := range durationKeys {
		metric := durations[key]
		cumulative := uint64(0)
		for index, upperBound := range requestDurationBuckets {
			cumulative += metric.buckets[index]
			fmt.Fprintf(
				&output,
				"error_tracer_http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d\n",
				metricLabel(key.method), metricLabel(key.route),
				strconv.FormatFloat(upperBound, 'g', -1, 64), cumulative,
			)
		}
		cumulative += metric.buckets[len(requestDurationBuckets)]
		fmt.Fprintf(
			&output,
			"error_tracer_http_request_duration_seconds_bucket{method=%q,route=%q,le=\"+Inf\"} %d\n",
			metricLabel(key.method), metricLabel(key.route), cumulative,
		)
		fmt.Fprintf(
			&output,
			"error_tracer_http_request_duration_seconds_sum{method=%q,route=%q} %s\n",
			metricLabel(key.method), metricLabel(key.route),
			strconv.FormatFloat(metric.sum, 'g', -1, 64),
		)
		fmt.Fprintf(
			&output,
			"error_tracer_http_request_duration_seconds_count{method=%q,route=%q} %d\n",
			metricLabel(key.method), metricLabel(key.route), metric.count,
		)
	}
	output.WriteString("# HELP error_tracer_http_in_flight_requests HTTP requests currently being handled.\n")
	output.WriteString("# TYPE error_tracer_http_in_flight_requests gauge\n")
	fmt.Fprintf(&output, "error_tracer_http_in_flight_requests %d\n", m.inFlight.Load())
	output.WriteString("# HELP error_tracer_ingested_events_total Events committed to the issue store.\n")
	output.WriteString("# TYPE error_tracer_ingested_events_total counter\n")
	fmt.Fprintf(&output, "error_tracer_ingested_events_total %d\n", m.ingested.Load())
	output.WriteString("# HELP error_tracer_ready Whether the service is ready to accept work.\n")
	output.WriteString("# TYPE error_tracer_ready gauge\n")
	fmt.Fprintf(&output, "error_tracer_ready %d\n", boolMetric(ready))
	output.WriteString("# HELP error_tracer_store_ready Whether the issue store passed its latest readiness probe.\n")
	output.WriteString("# TYPE error_tracer_store_ready gauge\n")
	fmt.Fprintf(&output, "error_tracer_store_ready %d\n", boolMetric(storeReady))
	output.WriteString("# HELP error_tracer_demo_enabled Whether the public read-only demo is enabled.\n")
	output.WriteString("# TYPE error_tracer_demo_enabled gauge\n")
	fmt.Fprintf(&output, "error_tracer_demo_enabled %d\n", boolMetric(demo))
	return output.String()
}

type metricsResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func metricMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func metricRoute(pattern string) string {
	if _, route, found := strings.Cut(pattern, " "); found && route != "" {
		return route
	}
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}

func metricLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
}
