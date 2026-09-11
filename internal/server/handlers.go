package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
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
		Lang   string `json:"lang"` // language code selected in the browser
	}

	if err := decode(w, r, maxAudioRequest, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if in.Format == "" {
		in.Format = "wav"
	}

	tr, err := s.speech.Transcribe(r.Context(), in.Audio, in.Format, lang.Lookup(in.Lang))
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"text": tr.Text, "cost_usd": tr.CostUSD})
}

func (s *Server) handleSpeak(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
		Lang string `json:"lang"` // language the text is written in
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if strings.TrimSpace(in.Text) == "" {
		writeError(w, r, http.StatusBadRequest, errors.New("nothing to say"))

		return
	}

	clip, err := s.speech.Speak(r.Context(), in.Text, lang.Lookup(in.Lang))
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err)

		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("X-Generation-Id", clip.GenerationID)
	_, _ = w.Write(clip.Audio)
}

// handleSummarize shortens a passage selected in a full answer, so the
// browser can have it spoken.
func (s *Server) handleSummarize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
		Lang string `json:"lang"` // language the summary is to be spoken in
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if strings.TrimSpace(in.Text) == "" {
		writeError(w, r, http.StatusBadRequest, errors.New("nothing to summarize"))

		return
	}

	sum, err := s.speech.Summarize(r.Context(), in.Text, lang.Lookup(in.Lang))
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err)

		return
	}

	// text is shown, speech is what goes back to /api/speak: the same words
	// with the speech tags the voice performs.
	writeJSON(w, http.StatusOK, map[string]any{"text": sum.Text, "speech": sum.Speech, "cost_usd": sum.CostUSD})
}

// handleCost prices a speech generation once OpenRouter has the record.
func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, r, http.StatusBadRequest, errors.New("missing id"))

		return
	}

	cost, err := s.speech.GenerationCost(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err)

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
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if !s.prompts.answer(in.ID, agent.Reply{Choices: in.Choices}) {
		writeError(w, r, http.StatusNotFound, errors.New("that question is no longer waiting for an answer"))

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
		Text      string `json:"text"`
		Selection string `json:"selection"` // passage of an earlier answer the question is about
		Lang      string `json:"lang"`      // language the question was asked in
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		writeError(w, r, http.StatusBadRequest, errors.New("empty question"))

		return
	}

	if !s.busy.CompareAndSwap(false, true) {
		writeError(w, r, http.StatusConflict, errors.New("a question is already in progress"))

		return
	}

	// Deliberately detached from the request: this work outlives it.
	ctx := context.WithoutCancel(r.Context())
	turn := s.session.startTurn(in.Text, agent.Excerpt(in.Selection))
	req := agent.Request{Text: in.Text, Selection: in.Selection, Language: lang.Lookup(in.Lang)}

	go s.runTurn(ctx, turn, req)

	writeJSON(w, http.StatusAccepted, map[string]int{"turn": turn})
}

// runTurn works through one question and records how it ended. Whatever
// happens, the turn ends with an entry on the stream: a page left waiting on
// silence is the one outcome the UI cannot explain.
func (s *Server) runTurn(ctx context.Context, turn int, req agent.Request) {
	started := time.Now()

	defer s.busy.Store(false)
	defer s.recoverTurn(turn)

	slog.Debug("turn started", "turn", turn, "lang", req.Language.Code)

	answer, err := s.agent.Ask(ctx, req, &turnHandler{turn: turn, log: s.session, desk: s.prompts})

	slog.Debug("turn finished", "turn", turn, "ms", time.Since(started).Milliseconds(), "error", err)

	switch {
	case errors.Is(err, agent.ErrCancelled), errors.Is(err, context.Canceled):
		s.session.add(turn, kindError, map[string]string{keyMessage: "cancelled", "code": "cancelled"})
	case err != nil:
		slog.Error("ask failed", "turn", turn, "error", err)
		s.session.add(turn, kindError, map[string]string{keyMessage: err.Error()})
	default:
		s.session.add(turn, kindResult, answer)
	}
}

// recoverTurn turns a panic in the agent into a visible failure instead of a
// dead process and a page that waits for ever.
func (s *Server) recoverTurn(turn int) {
	p := recover()
	if p == nil {
		return
	}

	slog.Error("turn panicked", "turn", turn, "panic", p, "stack", string(debug.Stack()))
	s.session.add(turn, kindError, map[string]string{keyMessage: fmt.Sprintf("the agent crashed: %v", p)})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	doc, err := s.settings.Load()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	doc, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsRequest))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if err := s.settings.Save(doc); err != nil {
		writeError(w, r, http.StatusBadRequest, err)

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
