package server

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/soulteary/Error-Tracer/internal/store"
)

const (
	maxIssueOffset         = 100_000
	maxIssueUpdateBodySize = 4 * 1024
	issueCursorVersion     = 1
	issueCursorSize        = 1 + 8 + 32
	eventCursorVersion     = 1
	eventCursorSize        = 1 + 8 + 8
)

type issueResponse struct {
	Issue store.Issue `json:"issue"`
}

type issuePageResponse struct {
	store.IssuePage
	NextCursor string `json:"next_cursor,omitempty"`
}

type eventPageResponse struct {
	store.EventPage
	NextCursor string `json:"next_cursor,omitempty"`
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
	writeIssuePage(w, page)
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

func (s *Server) listIssueEvents(w http.ResponseWriter, request *http.Request) {
	if !s.authorizeAdmin(w, request) {
		return
	}
	fingerprint := request.PathValue("fingerprint")
	if !validFingerprint(fingerprint) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_fingerprint"})
		return
	}
	options, err := parseEventListOptions(request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_pagination"})
		return
	}
	page, err := s.store.ListIssueEvents(request.Context(), s.projectID, fingerprint, options)
	if errors.Is(err, store.ErrIssueNotFound) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "issue_not_found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	writeEventPage(w, page)
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
		if bodyLimitExceeded(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "request_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		if bodyLimitExceeded(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "request_too_large"})
			return
		}
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
	if len(query["limit"]) > 1 || len(query["offset"]) > 1 || len(query["cursor"]) > 1 {
		return store.ListOptions{}, errors.New("pagination parameters must not be repeated")
	}
	if len(query["status"]) > 1 {
		return store.ListOptions{}, store.ErrInvalidStatus
	}

	var options store.ListOptions
	if _, hasOffset := query["offset"]; hasOffset && query.Get("cursor") != "" {
		return store.ListOptions{}, errors.New("offset and cursor are mutually exclusive")
	}
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
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeIssueCursor(raw)
		if err != nil {
			return store.ListOptions{}, err
		}
		options.After = &cursor
	}
	if raw := query.Get("status"); raw != "" {
		options.Status = store.IssueStatus(raw)
		if !options.Status.Valid() {
			return store.ListOptions{}, store.ErrInvalidStatus
		}
	}
	return options, nil
}

func parseEventListOptions(request *http.Request) (store.EventListOptions, error) {
	query := request.URL.Query()
	if len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
		return store.EventListOptions{}, errors.New("pagination parameters must not be repeated")
	}
	var options store.EventListOptions
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return store.EventListOptions{}, errors.New("limit must be between 1 and 100")
		}
		options.Limit = limit
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeEventCursor(raw)
		if err != nil {
			return store.EventListOptions{}, err
		}
		options.After = &cursor
	}
	return options, nil
}

func writeIssuePage(w http.ResponseWriter, page store.IssuePage) {
	writeJSON(w, http.StatusOK, issuePageResponse{
		IssuePage:  page,
		NextCursor: encodeIssueCursor(page.Next),
	})
}

func writeEventPage(w http.ResponseWriter, page store.EventPage) {
	writeJSON(w, http.StatusOK, eventPageResponse{
		EventPage:  page,
		NextCursor: encodeEventCursor(page.Next),
	})
}

func encodeIssueCursor(cursor *store.ListCursor) string {
	if cursor == nil {
		return ""
	}
	fingerprint, err := hex.DecodeString(cursor.Fingerprint)
	if err != nil || len(fingerprint) != 32 {
		return ""
	}
	encoded := make([]byte, issueCursorSize)
	encoded[0] = issueCursorVersion
	binary.BigEndian.PutUint64(encoded[1:9], uint64(cursor.LastSeen.UnixNano()))
	copy(encoded[9:], fingerprint)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeIssueCursor(value string) (store.ListCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != issueCursorSize || decoded[0] != issueCursorVersion {
		return store.ListCursor{}, errors.New("invalid cursor")
	}
	return store.ListCursor{
		LastSeen:    time.Unix(0, int64(binary.BigEndian.Uint64(decoded[1:9]))).UTC(),
		Fingerprint: hex.EncodeToString(decoded[9:]),
	}, nil
}

func encodeEventCursor(cursor *store.EventCursor) string {
	if cursor == nil || cursor.Sequence < 1 {
		return ""
	}
	encoded := make([]byte, eventCursorSize)
	encoded[0] = eventCursorVersion
	binary.BigEndian.PutUint64(encoded[1:9], uint64(cursor.ReceivedAt.UnixNano()))
	binary.BigEndian.PutUint64(encoded[9:17], uint64(cursor.Sequence))
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeEventCursor(value string) (store.EventCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != eventCursorSize || decoded[0] != eventCursorVersion {
		return store.EventCursor{}, errors.New("invalid event cursor")
	}
	sequence := int64(binary.BigEndian.Uint64(decoded[9:17]))
	if sequence < 1 {
		return store.EventCursor{}, errors.New("invalid event cursor")
	}
	return store.EventCursor{
		ReceivedAt: time.Unix(0, int64(binary.BigEndian.Uint64(decoded[1:9]))).UTC(),
		Sequence:   sequence,
	}, nil
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
