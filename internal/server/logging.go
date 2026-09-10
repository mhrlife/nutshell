package server

// Every failure the UI can hit ends up here. A voice interface has almost no
// room to explain itself, so the browser reports what went wrong to the
// terminal running nutshell: when the screen goes quiet, that log is the only
// place left to look.

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	maxLogRequest = 8 << 10
	maxLogField   = 2 << 10

	// keyMessage names the human-readable half of a failure, in the log and
	// in the error entries the page reads off the stream.
	keyMessage = "message"
)

// writeError answers the caller and records the failure. Anything the client
// caused is a warning; anything we caused is an error.
func writeError(w http.ResponseWriter, r *http.Request, status int, err error) {
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}

	slog.Log(r.Context(), level, "request failed",
		"method", r.Method, "path", r.URL.Path, "status", status, "error", err)

	writeJSON(w, status, map[string]string{"error": err.Error()})
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
		writeError(w, r, http.StatusBadRequest, err)

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

	slog.Log(r.Context(), level, "browser", args...)
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

// logAPI runs next and notes how the call ended. Successful calls are only
// visible with --debug; the failures already logged themselves.
func (s *Server) logAPI(next http.Handler, w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	next.ServeHTTP(rec, r)

	slog.Debug("api", "method", r.Method, "path", r.URL.Path,
		"status", rec.status, "ms", time.Since(started).Milliseconds())
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
