package speech

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

// The narrator sends speech tags whatever the voice; only a voice that
// performs them may receive them.
func TestSpeechInputTags(t *testing.T) {
	t.Parallel()

	const text = "[pause] <emphasis>Setup</emphasis>. Run it."

	if got := newTestClient(Config{TTSModel: "x-ai/grok-voice-tts-1.0"}).speechInput(text, lang.Language{}); got != text {
		t.Errorf("grok input = %q, want the tags kept", got)
	}

	gemini := newTestClient(Config{TTSModel: "google/gemini-3.1-flash-tts-preview", Style: DryStyle})
	if got, want := gemini.speechInput(text, lang.Language{}), SpeakInstructions(DryStyle, lang.Language{})+"Setup. Run it."; got != want {
		t.Errorf("gemini input = %q, want %q", got, want)
	}
}

// Grok ignores OpenRouter's top-level speed, so a faster voice depends on the
// value also arriving as an xAI provider option.
func TestSpeechRequestSpeed(t *testing.T) {
	t.Parallel()

	fast := newTestClient(Config{TTSModel: "x-ai/grok-voice-tts-1.0", Voice: "eve", Speed: 1.2}).speechRequest("hi")

	if fast["speed"] != 1.2 {
		t.Errorf("speed = %v", fast["speed"])
	}

	wantProvider := map[string]any{"options": map[string]any{"xai": map[string]any{"speed": 1.2}}}
	if !reflect.DeepEqual(fast["provider"], wantProvider) {
		t.Errorf("provider = %v", fast["provider"])
	}

	for _, speed := range []float64{0, 1} {
		body := newTestClient(Config{Speed: speed}).speechRequest("hi")
		if _, ok := body["speed"]; ok {
			t.Errorf("speed %v was sent: %v", speed, body)
		}

		if _, ok := body["provider"]; ok {
			t.Errorf("speed %v sent provider options: %v", speed, body)
		}
	}
}

// newTestClient builds a client whose logs go nowhere.
func newTestClient(cfg Config) *Client {
	return New(cfg, slog.New(slog.DiscardHandler))
}

// The point of a streaming voice is that the first words are available while
// the rest is still being spoken, so Speak must come back with the clip
// before the reply has finished arriving.
func TestSpeakStreams(t *testing.T) {
	t.Parallel()

	read := make(chan struct{}) // the test holds the first samples

	c, _ := fakeOpenRouter(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte("ab"))
		_ = http.NewResponseController(w).Flush()

		<-read

		_, _ = w.Write([]byte("cd"))
	})

	clip, err := c.Speak(t.Context(), "hello", lang.Language{})
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}

	defer clip.Audio.Close()

	first := make([]byte, 2)
	if _, err := io.ReadFull(clip.Audio, first); err != nil {
		t.Fatalf("reading the first samples: %v", err)
	}

	if string(first) != "ab" {
		t.Errorf("first samples = %q", first)
	}

	close(read) // the voice says the rest

	rest, err := io.ReadAll(clip.Audio)
	if err != nil {
		t.Fatalf("reading the rest: %v", err)
	}

	if string(rest) != "cd" {
		t.Errorf("rest of the clip = %q", rest)
	}
}

// A provider that accepts the request and then says nothing gets another try:
// the first sample is read while retrying is still possible.
func TestSpeakRetriesSilence(t *testing.T) {
	t.Parallel()

	c, calls := fakeOpenRouter(t, func(http.ResponseWriter) {})

	if _, err := c.Speak(t.Context(), "hello", lang.Language{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want %v", err, ErrUnavailable)
	}

	if n := calls.Load(); n != attempts {
		t.Errorf("OpenRouter was called %d times, want %d", n, attempts)
	}
}
