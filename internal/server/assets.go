package server

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"

	"github.com/soulteary/Error-Tracer/sdk"
)

var (
	browserSDKBody = sdk.BrowserScript()
	browserSDKETag = fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(browserSDKBody)))
)

func (s *Server) browserSDK(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("ETag", browserSDKETag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Header.Get("If-None-Match") == browserSDKETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, browserSDKBody)
}
