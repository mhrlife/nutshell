package speech

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

// LiteLLM serves Gemini's TTS models through /chat/completions with audio
// output; the samples arrive whole, as base64 in the JSON.
func TestSpeakOverChat(t *testing.T) {
	t.Parallel()

	type request struct {
		Path       string
		Model      string   `json:"model"`
		Modalities []string `json:"modalities"`
		Audio      struct {
			Voice  string `json:"voice"`
			Format string `json:"format"`
		} `json:"audio"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}

	seen := make(chan request, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := request{Path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&req)

		select {
		case seen <- req:
		default:
		}

		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":null,"audio":{"data":%q}}}]}`,
			base64.StdEncoding.EncodeToString([]byte("abcd")))
	}))
	t.Cleanup(ts.Close)

	c := New(Config{
		TTS:           Endpoint{BaseURL: ts.URL, APIKey: "k", Model: "gemini-3.1-flash-tts-preview"},
		Voice:         "Charon",
		SpeakOverChat: true,
	}, slog.New(slog.DiscardHandler))

	clip, err := c.Speak(t.Context(), "hello", lang.Language{})
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}

	defer clip.Audio.Close()

	if audio, err := io.ReadAll(clip.Audio); err != nil || string(audio) != "abcd" {
		t.Errorf("audio = %q, %v", audio, err)
	}

	req := <-seen
	if req.Path != chatPath || req.Model != "gemini-3.1-flash-tts-preview" || !slices.Equal(req.Modalities, []string{"audio"}) {
		t.Errorf("request = %+v", req)
	}

	if req.Audio.Voice != "Charon" || req.Audio.Format != "pcm16" {
		t.Errorf("audio options = %+v", req.Audio)
	}

	if len(req.Messages) != 1 || !strings.HasSuffix(req.Messages[0].Content, "hello") {
		t.Errorf("messages = %+v", req.Messages)
	}
}

// A reply without audio is worth another try, like a stream that says nothing.
func TestSpeakOverChatWithoutAudio(t *testing.T) {
	t.Parallel()

	c, calls := fakeOpenRouter(t, replyBody(`{"choices":[{"message":{"content":"I cannot read that aloud."}}]}`))
	c.cfg.SpeakOverChat = true

	if _, err := c.Speak(t.Context(), "hello", lang.Language{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want %v", err, ErrUnavailable)
	}

	if n := calls.Load(); n != attempts {
		t.Errorf("the provider was called %d times, want %d", n, attempts)
	}
}
