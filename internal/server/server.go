// Package server exposes the agent and the speech services to the browser UI
// over a small JSON and server-sent-events API.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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
	mux      *http.ServeMux
	agent    agent.Agent
	speech   Speech
	settings Settings
	cfg      Config
	busy     atomic.Bool
	prompts  *promptDesk
	session  *session
}

// New wires the routes. static serves the UI (index.html at its root).
func New(ag agent.Agent, sp Speech, st Settings, static http.FileSystem, cfg Config) *Server {
	s := &Server{
		mux: http.NewServeMux(), agent: ag, speech: sp, settings: st, cfg: cfg,
		prompts: newPromptDesk(), session: newSession(),
	}

	s.mux.Handle("GET /", http.FileServer(static))
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/stream", s.handleStream)
	s.mux.HandleFunc("POST /api/transcribe", s.handleTranscribe)
	s.mux.HandleFunc("POST /api/ask", s.handleAsk)
	s.mux.HandleFunc("POST /api/answer", s.handleAnswer)
	s.mux.HandleFunc("POST /api/cancel", s.handleCancel)
	s.mux.HandleFunc("POST /api/speak", s.handleSpeak)
	s.mux.HandleFunc("POST /api/summarize", s.handleSummarize)
	s.mux.HandleFunc("GET /api/cost", s.handleCost)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("POST /api/log", s.handleClientLog)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.mux.ServeHTTP(w, r)

		return
	}

	s.logAPI(s.mux, w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
