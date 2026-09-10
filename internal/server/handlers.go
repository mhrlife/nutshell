package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mhrlife/nutshell/internal/agent"
)

const (
	maxAudioRequest    = 32 << 20
	maxTextRequest     = 1 << 20
	maxSettingsRequest = 64 << 10
)

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"lang":    s.cfg.Lang,
		"project": s.cfg.Project,
		"agent":   s.agent.Name(),
		"voice":   s.speech.Enabled(),
		"busy":    s.busy.Load(),
	})
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Audio  string `json:"audio"`
		Format string `json:"format"`
		Hint   string `json:"hint"` // language the speaker most likely used, free text
	}

	if err := decode(w, r, maxAudioRequest, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if in.Format == "" {
		in.Format = "wav"
	}

	tr, err := s.speech.Transcribe(r.Context(), in.Audio, in.Format, in.Hint)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"text": tr.Text, "cost_usd": tr.CostUSD})
}

func (s *Server) handleSpeak(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if strings.TrimSpace(in.Text) == "" {
		writeError(w, http.StatusBadRequest, errors.New("nothing to say"))

		return
	}

	clip, err := s.speech.Speak(r.Context(), in.Text)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("X-Generation-Id", clip.GenerationID)
	_, _ = w.Write(clip.Audio)
}

// handleCost prices a speech generation once OpenRouter has the record.
func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing id"))

		return
	}

	cost, err := s.speech.GenerationCost(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]float64{"cost_usd": cost})
}

// handleAnswer delivers the user's reply to a prompt the agent is waiting on.
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string              `json:"id"`
		Choices map[string][]string `json:"choices"`
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if !s.prompts.answer(in.ID, agent.Reply{Choices: in.Choices}) {
		writeError(w, http.StatusNotFound, errors.New("that question is no longer waiting for an answer"))

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.agent.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

// handleAsk starts one turn and returns as soon as it is under way. The turn
// itself belongs to the session, so the browser can reload, go away, or come
// back while the agent works; everything it produces is written to the log and
// read from /api/stream.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		writeError(w, http.StatusBadRequest, errors.New("empty question"))

		return
	}

	if !s.busy.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, errors.New("a question is already in progress"))

		return
	}

	// Deliberately detached from the request: this work outlives it.
	ctx := context.WithoutCancel(r.Context())
	turn := s.session.startTurn(in.Text)

	go s.runTurn(ctx, turn, in.Text)

	writeJSON(w, http.StatusAccepted, map[string]int{"turn": turn})
}

// runTurn works through one question and records how it ended.
func (s *Server) runTurn(ctx context.Context, turn int, question string) {
	defer s.busy.Store(false)

	answer, err := s.agent.Ask(ctx, question, &turnHandler{turn: turn, log: s.session, desk: s.prompts})

	switch {
	case errors.Is(err, agent.ErrCancelled), errors.Is(err, context.Canceled):
		s.session.add(turn, kindError, map[string]string{"message": "cancelled", "code": "cancelled"})
	case err != nil:
		slog.Error("ask failed", "error", err)
		s.session.add(turn, kindError, map[string]string{"message": err.Error()})
	default:
		s.session.add(turn, kindResult, answer)
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	doc, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsRequest))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := s.settings.Save(doc); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}

	return nil
}
