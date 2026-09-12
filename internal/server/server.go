// Package server exposes the agent and the speech services to the browser UI
// over a small JSON and server-sent-events API.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
	"github.com/mhrlife/nutshell/internal/speech"
)

// Speech is what the server needs from the speech provider.
type Speech interface {
	Enabled() bool
	Transcribe(ctx context.Context, audioB64, format string, l lang.Language) (speech.Transcript, error)
	Speak(ctx context.Context, text string, l lang.Language) (speech.Clip, error)
	Summarize(ctx context.Context, passage string, l lang.Language) (speech.Summary, error)
	GenerationCost(ctx context.Context, id string) (float64, error)
}

// Settings persists the UI's preferences between launches.
type Settings interface {
	Load() (json.RawMessage, error)
	Save(doc json.RawMessage) error
}

// Config is what the UI learns about at startup.
type Config struct {
	Lang    string // default UI language
	Project string // working directory name shown in the header
}

// Server is the HTTP handler for the UI and its API.
type Server struct {
	handler  http.Handler
	agent    agent.Agent
	speech   Speech
	settings Settings
	cfg      Config
	logger   *slog.Logger
	busy     atomic.Bool
	prompts  *promptDesk
	session  *session
	threads  *threads
}

// New wires the routes. static serves the UI (index.html at its root), and
// logger receives everything the server logs.
func New(ag agent.Agent, sp Speech, st Settings, static http.FileSystem, cfg Config, logger *slog.Logger) *Server {
	s := &Server{
		agent: ag, speech: sp, settings: st, cfg: cfg, logger: logger,
		prompts: newPromptDesk(), session: newSession(logger), threads: newThreads(),
	}

	// From here on the agent can speak up on its own, not only when asked.
	ag.Watch(unasked{srv: s})

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(static))
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("POST /api/transcribe", s.handleTranscribe)
	mux.HandleFunc("POST /api/ask", s.handleAsk)
	mux.HandleFunc("POST /api/answer", s.handleAnswer)
	mux.HandleFunc("POST /api/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/close", s.handleClose)
	mux.HandleFunc("POST /api/speak", s.handleSpeak)
	mux.HandleFunc("POST /api/summarize", s.handleSummarize)
	mux.HandleFunc("GET /api/cost", s.handleCost)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/log", s.handleClientLog)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// Requests are logged before the guard sees them, so one it turns away
	// shows up like any other failure.
	s.handler = s.logRequests(s.guard(mux))

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.handler.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
