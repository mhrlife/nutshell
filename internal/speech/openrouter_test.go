package speech

import (
	"reflect"
	"testing"
)

// Grok ignores OpenRouter's top-level speed, so a faster voice depends on the
// value also arriving as an xAI provider option.
func TestSpeechRequestSpeed(t *testing.T) {
	t.Parallel()

	fast := New(Config{TTSModel: "x-ai/grok-voice-tts-1.0", Voice: "eve", Speed: 1.2}).speechRequest("hi")

	if fast["speed"] != 1.2 {
		t.Errorf("speed = %v", fast["speed"])
	}

	wantProvider := map[string]any{"options": map[string]any{"xai": map[string]any{"speed": 1.2}}}
	if !reflect.DeepEqual(fast["provider"], wantProvider) {
		t.Errorf("provider = %v", fast["provider"])
	}

	for _, speed := range []float64{0, 1} {
		body := New(Config{Speed: speed}).speechRequest("hi")
		if _, ok := body["speed"]; ok {
			t.Errorf("speed %v was sent: %v", speed, body)
		}

		if _, ok := body["provider"]; ok {
			t.Errorf("speed %v sent provider options: %v", speed, body)
		}
	}
}
