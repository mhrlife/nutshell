package server

// Every failure the UI can hit ends up here. A voice interface has almost no
// room to explain itself, so the browser reports what went wrong to the
// terminal running nutshell: when the screen goes quiet, that log is the only
// place left to look.

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mhrlife/nutshell/internal/speech"
)

const (
	maxLogRequest = 8 << 10
	maxLogField   = 2 << 10

	// keyMessage names the human-readable half of a failure, in the log and
	// in the error entries the page reads off the stream.
	keyMessage = "message"
)

// logRequests notes how each API call ended. Successful calls are only
// visible with --debug; the failures already logged themselves.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)

			return
		}

		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		s.logger.DebugContext(r.Context(), "api", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "ms", time.Since(started).Milliseconds())
	})
}

// writeError answers the caller and records the failure. Anything the client
// caused is a warning; anything we caused is an error.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, err error) {
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}

	s.logger.Log(r.Context(), level, "request failed",
		"method", r.Method, "path", r.URL.Path, "status", status, "error", err)

	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// speechFailureStatus is the status for a failed OpenRouter call: 503 when
// every retry was spent on a connection that kept failing, which the page
// reports as a connection problem, and 502 for anything OpenRouter refused.
func speechFailureStatus(err error) int {
	if errors.Is(err, speech.ErrUnavailable) {
		return http.StatusServiceUnavailable
	}

	return http.StatusBadGateway
}

// handleClientLog records something that went wrong in the browser. The UI is
// the half of nutshell the terminal cannot see, so it tells us itself.
func (s *Server) handleClientLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level   string `json:"level"` // "error" or "warn"
		Event   string `json:"event"` // where in the UI, e.g. "transcribe"
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}

	if err := decode(w, r, maxLogRequest, &in); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	level := slog.LevelWarn
	if in.Level == "error" {
		level = slog.LevelError
	}

	args := []any{"event", clamp(in.Event), keyMessage, clamp(in.Message)}
	if in.Detail != "" {
		args = append(args, "detail", clamp(in.Detail))
	}

	s.logger.Log(r.Context(), level, "browser", args...)
	w.WriteHeader(http.StatusNoContent)
}

// clamp keeps a browser-supplied string from flooding the terminal.
func clamp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLogField {
		return s
	}

	return s[:maxLogField] + "…"
}

// statusRecorder remembers the status code so the request log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// Flush keeps the event stream working through the wrapper.
func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
