package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/soulteary/Error-Tracer/internal/store"
)

const (
	maxIssueOffset         = 100_000
	maxIssueUpdateBodySize = 4 * 1024
)

type issueResponse struct {
	Issue store.Issue `json:"issue"`
}

type issueUpdateRequest struct {
	Status store.IssueStatus `json:"status"`
}

func (s *Server) listIssues(w http.ResponseWriter, request *http.Request) {
	if !s.authorizeAdmin(w, request) {
		return
	}
	options, err := parseListOptions(request)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStatus) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_status", Field: "status"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_pagination"})
		return
	}

	page, err := s.store.ListIssues(request.Context(), s.projectID, options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getIssue(w http.ResponseWriter, request *http.Request) {
	if !s.authorizeAdmin(w, request) {
		return
	}
	fingerprint := request.PathValue("fingerprint")
	if !validFingerprint(fingerprint) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_fingerprint"})
		return
	}

	issue, err := s.store.GetIssue(request.Context(), s.projectID, fingerprint)
	if errors.Is(err, store.ErrIssueNotFound) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "issue_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, issueResponse{Issue: issue})
}

func (s *Server) updateIssue(w http.ResponseWriter, request *http.Request) {
	if !s.authorizeAdmin(w, request) {
		return
	}
	fingerprint := request.PathValue("fingerprint")
	if !validFingerprint(fingerprint) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_fingerprint"})
		return
	}
	if !isJSONMediaType(request.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "unsupported_media_type"})
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, maxIssueUpdateBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload issueUpdateRequest
	if err := decoder.Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "request_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if !payload.Status.Valid() {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "invalid_status", Field: "status"})
		return
	}

	issue, err := s.store.SetIssueStatus(request.Context(), s.projectID, fingerprint, payload.Status)
	if errors.Is(err, store.ErrIssueNotFound) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "issue_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, issueResponse{Issue: issue})
}

func (s *Server) authorizeAdmin(w http.ResponseWriter, request *http.Request) bool {
	if s.store == nil || s.projectID == "" || len(s.adminTokens) == 0 || s.adminTokens[0] == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "admin_unavailable"})
		return false
	}
	scheme, token, found := strings.Cut(request.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || !constantTimeAny(token, s.adminTokens) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="error-tracer"`)
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
		return false
	}
	return true
}

func parseListOptions(request *http.Request) (store.ListOptions, error) {
	query := request.URL.Query()
	if len(query["limit"]) > 1 || len(query["offset"]) > 1 {
		return store.ListOptions{}, errors.New("pagination parameters must not be repeated")
	}
	if len(query["status"]) > 1 {
		return store.ListOptions{}, store.ErrInvalidStatus
	}

	var options store.ListOptions
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return store.ListOptions{}, errors.New("limit must be between 1 and 100")
		}
		options.Limit = limit
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > maxIssueOffset {
			return store.ListOptions{}, errors.New("offset is out of range")
		}
		options.Offset = offset
	}
	if raw := query.Get("status"); raw != "" {
		options.Status = store.IssueStatus(raw)
		if !options.Status.Valid() {
			return store.ListOptions{}, store.ErrInvalidStatus
		}
	}
	return options, nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func isJSONMediaType(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == "application/json"
}
