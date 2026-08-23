package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"bystander/internal/store"
)

// maxRequestBody bounds what will be decoded. Every request this API takes is a handful
// of fields; anything larger is a mistake or an attempt.
const maxRequestBody = 64 << 10

// errorBody is the shape of every refusal: {"error": "a sentence"}.
//
// A sentence, written for the person who will read it in the interface — not a code for a
// client to switch on, because the only client is the one shipped in this image.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON sends a value, or a 204 when there is nothing to send.
func writeJSON(w http.ResponseWriter, status int, body any) {
	if body == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError sends a refusal.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// fail maps a store error onto a status code.
//
// The one place that translation happens. A handler that switched on a driver error, or
// invented its own mapping, would eventually disagree with another handler about what a
// duplicate username is.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		// The message is not sent on: an unexpected error is a bug or an outage, and its
		// text is for the log rather than for a stranger's browser.
		s.log.Error("request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong at our end")
	}
}

// decode reads a JSON body into v, or writes the refusal itself and returns false.
//
// DisallowUnknownFields is deliberate: a client sending "priorty" should be told, not
// silently ignored and left wondering why the value never changed.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, readable(err))
		return false
	}
	return true
}

// readable turns a decoder error into something worth showing somebody.
func readable(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Sprintf("%s should be %s", typeErr.Field, typeErr.Type)
	}
	if errors.Is(err, io.EOF) {
		return "the request had no body"
	}
	return "the request body could not be read as JSON"
}

// logAttrs is the standard set for a request-scoped log line.
func logAttrs(r *http.Request) []slog.Attr {
	return []slog.Attr{slog.String("method", r.Method), slog.String("path", r.URL.Path)}
}
