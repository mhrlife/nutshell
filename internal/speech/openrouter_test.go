package speech

import (
	"log/slog"
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

	gemini := newTestClient(Config{TTSModel: "google/gemini-3.1-flash-tts-preview", Style: LectureStyle})
	if got, want := gemini.speechInput(text, lang.Language{}), SpeakInstructions(LectureStyle, lang.Language{})+"Setup. Run it."; got != want {
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
