package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if in.Format == "" {
		in.Format = "wav"
	}

	tr, err := s.speech.Transcribe(r.Context(), in.Audio, in.Format, lang.Lookup(in.Lang))
	if err != nil {
		s.writeError(w, r, speechFailureStatus(err), err)

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
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if strings.TrimSpace(in.Text) == "" {
		s.writeError(w, r, http.StatusBadRequest, errors.New("nothing to say"))

		return
	}

	clip, err := s.speech.Speak(r.Context(), in.Text, lang.Lookup(in.Lang))
	if err != nil {
		s.writeError(w, r, speechFailureStatus(err), err)

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
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if strings.TrimSpace(in.Text) == "" {
		s.writeError(w, r, http.StatusBadRequest, errors.New("nothing to summarize"))

		return
	}

	sum, err := s.speech.Summarize(r.Context(), in.Text, lang.Lookup(in.Lang))
	if err != nil {
		s.writeError(w, r, speechFailureStatus(err), err)

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
		s.writeError(w, r, http.StatusBadRequest, errors.New("missing id"))

		return
	}

	cost, err := s.speech.GenerationCost(r.Context(), id)
	if err != nil {
		s.writeError(w, r, speechFailureStatus(err), err)

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
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if !s.prompts.answer(in.ID, agent.Reply{Choices: in.Choices}) {
		s.writeError(w, r, http.StatusNotFound, errors.New("that question is no longer waiting for an answer"))

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	doc, err := s.settings.Load()
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	doc, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsRequest))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if err := s.settings.Save(doc); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)

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
