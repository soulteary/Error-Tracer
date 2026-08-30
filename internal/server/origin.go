package server

import (
	"net/http"
	"strings"
)

func (s *Server) ingestEventWithOrigin(w http.ResponseWriter, request *http.Request) {
	if !s.allowIngestRequest(w, request) {
		return
	}
	s.ingestEvent(w, request)
}

func (s *Server) ingestBatchWithOrigin(w http.ResponseWriter, request *http.Request) {
	if !s.allowIngestRequest(w, request) {
		return
	}
	s.ingestBatch(w, request)
}

func (s *Server) allowIngestRequest(w http.ResponseWriter, request *http.Request) bool {
	if !s.allowEventOrigin(w, request) {
		return false
	}
	return allowRateLimit(w, request, s.requestLimiter, 1)
}

func (s *Server) allowIngestTokens(
	w http.ResponseWriter,
	request *http.Request,
	tokens int,
) bool {
	return allowRateLimit(w, request, s.ingestLimiter, tokens)
}

func allowRateLimit(
	w http.ResponseWriter,
	request *http.Request,
	limiter *rateLimiter,
	tokens int,
) bool {
	allowed, retryAfter := limiter.AllowN(clientAddress(request.RemoteAddr), tokens)
	if !allowed {
		w.Header().Set("Retry-After", retryAfterHeader(retryAfter))
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "rate_limited"})
		return false
	}
	return true
}

func (s *Server) preflightEvent(w http.ResponseWriter, request *http.Request) {
	if !s.allowEventOrigin(w, request) {
		return
	}
	if request.Header.Get("Access-Control-Request-Method") != http.MethodPost ||
		!allowedPreflightHeaders(request.Header.Get("Access-Control-Request-Headers")) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "origin_not_allowed"})
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) allowEventOrigin(w http.ResponseWriter, request *http.Request) bool {
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		if request.Method == http.MethodOptions {
			writeJSON(w, http.StatusForbidden, errorResponse{Error: "origin_not_allowed"})
			return false
		}
		return true
	}
	if len(origins) != 1 {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "origin_not_allowed"})
		return false
	}
	origin := strings.ToLower(strings.TrimSpace(origins[0]))
	if _, allowed := s.allowedOrigins[origin]; !allowed {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "origin_not_allowed"})
		return false
	}
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	return true
}

func allowedPreflightHeaders(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	for _, header := range strings.Split(value, ",") {
		if !strings.EqualFold(strings.TrimSpace(header), "Content-Type") {
			return false
		}
	}
	return true
}
