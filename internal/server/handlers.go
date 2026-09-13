package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
	"github.com/mhrlife/nutshell/internal/speech"
)

const (
	maxAudioRequest    = 32 << 20
	maxTextRequest     = 1 << 20
	maxSettingsRequest = 64 << 10
	// audioChunk is how much spoken audio is passed on at a time, a fifth of
	// a second of it: small enough that the first words leave without a wait.
	audioChunk = speech.PCMSampleRate / 5 * 2
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
	defer clip.Audio.Close()

	// Raw samples, passed on as they arrive. The browser asks for a passage a
	// piece at a time and lays the pieces end to end, because the voice says
	// nothing until it has generated nearly everything — see static/pcm.js.
	w.Header().Set("Content-Type", "audio/pcm")
	w.Header().Set("X-Sample-Rate", strconv.Itoa(speech.PCMSampleRate))
	w.Header().Set("X-Generation-Id", clip.GenerationID)
	s.streamAudio(w, r, clip.Audio)
}

// streamAudio copies a clip to the browser chunk by chunk, flushing each one:
// holding audio back to send it in one piece is exactly what streaming is
// there to avoid. The reply has already gone out with its status, so a
// failure part way through can only be logged, and the browser hears a clip
// that stops early.
func (s *Server) streamAudio(w http.ResponseWriter, r *http.Request, audio io.Reader) {
	out := http.NewResponseController(w)
	buf := make([]byte, audioChunk)

	for {
		n, err := audio.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				s.logger.WarnContext(r.Context(), "speech stream cut off", "error", werr)

				return
			}

			if ferr := out.Flush(); ferr != nil {
				s.logger.WarnContext(r.Context(), "speech stream cannot be flushed", "error", ferr)

				return
			}
		}

		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.WarnContext(r.Context(), "speech stream broke off", "error", err)
			}

			return
		}
	}
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

	writeJSON(w, http.StatusOK, map[string]any{"text": sum.Text, "cost_usd": sum.CostUSD})
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
