package server

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/soulteary/Error-Tracer/internal/store"
)

// Server owns the HTTP routes and service readiness state.
type Server struct {
	handler        http.Handler
	ready          atomic.Bool
	store          store.Store
	demoStore      store.Store
	projectID      string
	ingestKey      string
	adminTokens    []string
	allowedOrigins map[string]struct{}
	ingestLimiter  *rateLimiter
	metrics        *serviceMetrics
	now            func() time.Time
	newID          func() (string, error)
}

// Options provides the dependencies required by the HTTP service.
type Options struct {
	Store              store.Store
	ProjectID          string
	IngestKey          string
	AdminToken         string
	PreviousAdminToken string
	AllowedOrigins     []string
	RatePerMinute      int
	RateBurst          int
	DemoMode           bool
	MetricsEnabled     bool
}

// New creates a service with liveness and readiness endpoints.
func New(options Options) *Server {
	allowedOrigins := make(map[string]struct{}, len(options.AllowedOrigins))
	for _, origin := range options.AllowedOrigins {
		allowedOrigins[origin] = struct{}{}
	}
	adminTokens := []string{options.AdminToken}
	if options.PreviousAdminToken != "" && options.PreviousAdminToken != options.AdminToken {
		adminTokens = append(adminTokens, options.PreviousAdminToken)
	}
	server := &Server{
		store:          options.Store,
		projectID:      options.ProjectID,
		ingestKey:      options.IngestKey,
		adminTokens:    adminTokens,
		allowedOrigins: allowedOrigins,
		ingestLimiter:  newRateLimiter(options.RatePerMinute, options.RateBurst),
		now:            time.Now,
		newID:          randomEventID,
	}
	if options.DemoMode {
		server.demoStore = newDemoStore(server.now().UTC())
	}
	if options.MetricsEnabled {
		server.metrics = newServiceMetrics()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.readiness)
	if server.metrics != nil {
		mux.HandleFunc("GET /metrics", server.prometheusMetrics)
	}
	mux.HandleFunc("GET /{$}", server.dashboard)
	mux.HandleFunc("GET /assets/dashboard.css", server.dashboardCSS)
	mux.HandleFunc("GET /assets/dashboard.js", server.dashboardJS)
	mux.HandleFunc("GET /assets/error-tracer.js", server.browserSDK)
	mux.HandleFunc("GET /api/v1/meta", server.publicMetadata)
	mux.HandleFunc("GET /api/v1/demo/issues", server.listDemoIssues)
	mux.HandleFunc("GET /api/v1/demo/issues/{fingerprint}", server.getDemoIssue)
	mux.HandleFunc("POST /api/v1/events", server.ingestEventWithOrigin)
	mux.HandleFunc("OPTIONS /api/v1/events", server.preflightEvent)
	mux.HandleFunc("POST /api/v1/events/batch", server.ingestBatchWithOrigin)
	mux.HandleFunc("OPTIONS /api/v1/events/batch", server.preflightEvent)
	mux.HandleFunc("GET /api/v1/issues", server.listIssues)
	mux.HandleFunc("GET /api/v1/issues/{fingerprint}", server.getIssue)
	mux.HandleFunc("PATCH /api/v1/issues/{fingerprint}", server.updateIssue)
	server.handler = mux
	if server.metrics != nil {
		server.handler = server.metrics.middleware(mux)
	}
	server.ready.Store(options.Store != nil && options.ProjectID != "" && options.IngestKey != "" && options.AdminToken != "")
	return server
}

// Handler returns the service HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// SetReady updates whether the process can accept new work.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeStatus(w, http.StatusOK, "ok")
}

func (s *Server) readiness(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeStatus(w, http.StatusOK, "ready")
}

func writeStatus(w http.ResponseWriter, statusCode int, status string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_, _ = io.WriteString(w, `{"status":"`+status+`"}`+"\n")
}
