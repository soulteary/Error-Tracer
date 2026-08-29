package server

import (
	"io"
	"net/http"
	"sync/atomic"
)

// Server owns the HTTP routes and service readiness state.
type Server struct {
	handler http.Handler
	ready   atomic.Bool
}

// New creates a service with liveness and readiness endpoints.
func New() *Server {
	server := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.readiness)
	server.handler = mux
	server.ready.Store(true)
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
