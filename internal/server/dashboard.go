package server

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io"
	"net/http"
)

//go:embed dashboard/*
var dashboardFiles embed.FS

type dashboardAsset struct {
	body        string
	contentType string
	etag        string
	cache       string
}

var (
	dashboardIndex  = loadDashboardAsset("dashboard/index.html", "text/html; charset=utf-8", "no-store")
	dashboardStyle  = loadDashboardAsset("dashboard/dashboard.css", "text/css; charset=utf-8", "public, max-age=300")
	dashboardScript = loadDashboardAsset("dashboard/dashboard.js", "text/javascript; charset=utf-8", "public, max-age=300")
)

func (s *Server) dashboard(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		dashboardSecurityHeaders(w)
		http.NotFound(w, request)
		return
	}
	serveDashboardAsset(w, request, dashboardIndex)
}

func (s *Server) dashboardCSS(w http.ResponseWriter, request *http.Request) {
	serveDashboardAsset(w, request, dashboardStyle)
}

func (s *Server) dashboardJS(w http.ResponseWriter, request *http.Request) {
	serveDashboardAsset(w, request, dashboardScript)
}

func serveDashboardAsset(w http.ResponseWriter, request *http.Request, asset dashboardAsset) {
	dashboardSecurityHeaders(w)
	w.Header().Set("Cache-Control", asset.cache)
	w.Header().Set("Content-Type", asset.contentType)
	w.Header().Set("ETag", asset.etag)
	if request.Header.Get("If-None-Match") == asset.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, asset.body)
}

func dashboardSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func loadDashboardAsset(path, contentType, cache string) dashboardAsset {
	body, err := dashboardFiles.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("load embedded dashboard asset %s: %v", path, err))
	}
	return dashboardAsset{
		body:        string(body),
		contentType: contentType,
		etag:        fmt.Sprintf(`"%x"`, sha256.Sum256(body)),
		cache:       cache,
	}
}
