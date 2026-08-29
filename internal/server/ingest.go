package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/soulteary/Error-Tracer/internal/event"
)

const (
	maxEventBodySize = 128 * 1024
	maxBatchBodySize = 1024 * 1024
	maxBatchEvents   = 100
)

type ingestRequest struct {
	ProjectKey string      `json:"project_key"`
	Event      event.Event `json:"event"`
}

type batchIngestRequest struct {
	ProjectKey string        `json:"project_key"`
	Events     []event.Event `json:"events"`
}

type ingestResponse struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

type batchIngestResponse struct {
	Events []ingestResponse `json:"events"`
}

type errorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func (s *Server) ingestEvent(w http.ResponseWriter, request *http.Request) {
	if s.store == nil || s.projectID == "" || s.ingestKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "ingest_unavailable"})
		return
	}
	if !supportedMediaType(request.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "unsupported_media_type"})
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, maxEventBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var payload ingestRequest
	if err := decoder.Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "event_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if !constantTimeEqual(payload.ProjectKey, s.ingestKey) {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "invalid_project_key"})
		return
	}

	captured, err := s.prepareEvent(payload.Event, request.UserAgent())
	if err != nil {
		var validationErr *event.ValidationError
		if errors.As(err, &validationErr) {
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
				Error: "invalid_event",
				Field: validationErr.Field,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}

	issue, err := s.store.Record(request.Context(), s.projectID, captured)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}

	writeJSON(w, http.StatusAccepted, ingestResponse{
		ID:          captured.ID,
		Fingerprint: issue.Fingerprint,
	})
}

func (s *Server) ingestBatch(w http.ResponseWriter, request *http.Request) {
	if s.store == nil || s.projectID == "" || s.ingestKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "ingest_unavailable"})
		return
	}
	if !supportedMediaType(request.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "unsupported_media_type"})
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, maxBatchBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var payload batchIngestRequest
	if err := decoder.Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "batch_too_large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json"})
		return
	}
	if !constantTimeEqual(payload.ProjectKey, s.ingestKey) {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "invalid_project_key"})
		return
	}
	if len(payload.Events) == 0 || len(payload.Events) > maxBatchEvents {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
			Error: "invalid_batch",
			Field: "events",
		})
		return
	}

	captured := make([]event.Event, len(payload.Events))
	for index, item := range payload.Events {
		prepared, err := s.prepareEvent(item, request.UserAgent())
		if err != nil {
			var validationErr *event.ValidationError
			if errors.As(err, &validationErr) {
				writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
					Error: "invalid_event",
					Field: fmt.Sprintf("events[%d].%s", index, validationErr.Field),
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
			return
		}
		captured[index] = prepared
	}

	issues, err := s.store.RecordBatch(request.Context(), s.projectID, captured)
	if err != nil || len(issues) != len(captured) {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	response := batchIngestResponse{Events: make([]ingestResponse, len(captured))}
	for index := range captured {
		response.Events[index] = ingestResponse{
			ID:          captured[index].ID,
			Fingerprint: issues[index].Fingerprint,
		}
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) prepareEvent(captured event.Event, userAgent string) (event.Event, error) {
	captured.ID = ""
	captured.ReceivedAt = s.now().UTC()
	captured.UserAgent = userAgent
	captured.Normalize()
	if err := captured.Validate(); err != nil {
		return event.Event{}, err
	}
	id, err := s.newID()
	if err != nil {
		return event.Event{}, err
	}
	captured.ID = id
	return captured, nil
}

func supportedMediaType(header string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || mediaType == "text/plain"
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func constantTimeEqual(left, right string) bool {
	leftDigest := sha256.Sum256([]byte(left))
	rightDigest := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftDigest[:], rightDigest[:]) == 1
}

func randomEventID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate event ID: %w", err)
	}
	return "evt_" + hex.EncodeToString(value[:]), nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
