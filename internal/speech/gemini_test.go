package speech

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/lang"
)

// geminiRequest is what the fake Gemini reads out of a request.
type geminiRequest struct {
	Path, Query, Key  string
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction"`
	GenerationConfig  struct {
		AudioTranscriptionConfig *struct {
			Mode string `json:"mode"`
		} `json:"audioTranscriptionConfig"`
		SpeechConfig struct {
			VoiceConfig struct {
				PrebuiltVoiceConfig struct {
					VoiceName string `json:"voiceName"`
				} `json:"prebuiltVoiceConfig"`
			} `json:"voiceConfig"`
		} `json:"speechConfig"`
	} `json:"generationConfig"`
}

// fakeGemini returns a client whose every role runs on a server that answers
// with body, and the channel the last request it saw arrives on.
func fakeGemini(t *testing.T, cfg Config, body string) (*Client, <-chan geminiRequest) {
	t.Helper()

	seen := make(chan geminiRequest, attempts)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := geminiRequest{Path: r.URL.Path, Query: r.URL.RawQuery, Key: r.Header.Get("X-Goog-Api-Key")}
		_ = json.NewDecoder(r.Body).Decode(&req)

		select {
		case seen <- req:
		default:
		}

		fmt.Fprint(w, body)
	}))
	t.Cleanup(ts.Close)

	for _, ep := range []*Endpoint{&cfg.STT, &cfg.Summary, &cfg.TTS} {
		ep.Interface, ep.BaseURL, ep.APIKey = InterfaceGemini, ts.URL, "gemini-key"
	}

	c := New(cfg, slog.New(slog.DiscardHandler))
	c.retryDelay = time.Millisecond

	return c, seen
}

// audioEvent is one server-sent event carrying samples at rate.
func audioEvent(samples string, rate int) string {
	return fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":"+
		"{\"mimeType\":\"audio/L16;codec=pcm;rate=%d\",\"data\":%q}}]}}]}\r\n\r\n",
		rate, base64.StdEncoding.EncodeToString([]byte(samples)))
}

func TestGeminiSpeak(t *testing.T) {
	t.Parallel()

	cfg := Config{TTS: Endpoint{Model: "gemini-3.1-flash-tts-preview"}, Voice: "Algenib"}
	c, seen := fakeGemini(t, cfg, audioEvent("ab", PCMSampleRate)+audioEvent("cd", PCMSampleRate))

	clip, err := c.Speak(t.Context(), "hello", lang.Language{})
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}

	defer clip.Audio.Close()

	audio, err := io.ReadAll(clip.Audio)
	if err != nil || string(audio) != "abcd" {
		t.Errorf("audio = %q, %v; want the samples of both events", audio, err)
	}

	req := <-seen
	if req.Path != "/models/gemini-3.1-flash-tts-preview:streamGenerateContent" || req.Query != "alt=sse" {
		t.Errorf("called %s?%s", req.Path, req.Query)
	}

	if req.Key != "gemini-key" {
		t.Errorf("x-goog-api-key = %q", req.Key)
	}

	if v := req.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName; v != "Algenib" {
		t.Errorf("voice = %q", v)
	}

	if clip.GenerationID != "" {
		t.Errorf("Gemini has no generation to price, got ID %q", clip.GenerationID)
	}
}

func TestCheckPCM(t *testing.T) {
	t.Parallel()

	for mimeType, ok := range map[string]bool{
		"audio/L16;codec=pcm;rate=24000":    true, // the stream's name for it
		"audio/l16; rate=24000; channels=1": true, // generateContent's name for it
		"audio/l16; rate=24000; channels=2": false,
		"audio/L16;codec=pcm;rate=16000":    false,
		"audio/mpeg":                        false,
	} {
		if err := checkPCM(mimeType); (err == nil) != ok {
			t.Errorf("checkPCM(%q) = %v, want accepted: %v", mimeType, err, ok)
		}
	}
}

// Samples at another rate would play at the wrong speed, so they are refused.
func TestGeminiSpeakOtherRate(t *testing.T) {
	t.Parallel()

	c, _ := fakeGemini(t, Config{TTS: Endpoint{Model: "tts"}}, audioEvent("ab", 16000))

	if _, err := c.Speak(t.Context(), "hello", lang.Language{}); err == nil || !strings.Contains(err.Error(), "16000") {
		t.Errorf("err = %v, want one naming the rate", err)
	}
}

func TestGeminiTranscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		transcription string
		reply         string
		wantParts     int // a chat model gets instructions ahead of the recording
	}{
		{
			name:      "chat model",
			reply:     `{"candidates":[{"content":{"parts":[{"text":"hello "},{"text":"world"}]}}]}`,
			wantParts: 2,
		},
		{
			// the shape gemini-3.5-transcribe answers in
			name:          "transcription model",
			transcription: "SMART",
			reply:         `{"candidates":[{"content":{"parts":[{"audioTranscription":{"text":"hello world"}}],"role":"model"},"finishReason":"STOP"}]}`,
			wantParts:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{STT: Endpoint{Model: "stt"}, Transcription: tt.transcription}
			c, seen := fakeGemini(t, cfg, tt.reply)

			got, err := c.Transcribe(t.Context(), "AAAA", "wav", lang.Language{})
			if err != nil || got.Text != "hello world" {
				t.Fatalf("Transcribe = %q, %v", got.Text, err)
			}

			req := <-seen
			if req.Path != "/models/stt:generateContent" || len(req.Contents) != 1 {
				t.Fatalf("request = %+v", req)
			}

			parts := req.Contents[0].Parts
			if len(parts) != tt.wantParts || parts[len(parts)-1].InlineData == nil ||
				parts[len(parts)-1].InlineData.MIMEType != "audio/wav" {
				t.Errorf("parts = %+v", parts)
			}

			mode := ""
			if atc := req.GenerationConfig.AudioTranscriptionConfig; atc != nil {
				mode = atc.Mode
			}

			if mode != tt.transcription {
				t.Errorf("transcription mode = %q, want %q", mode, tt.transcription)
			}
		})
	}
}

func TestGeminiSummarize(t *testing.T) {
	t.Parallel()

	c, seen := fakeGemini(t, Config{Summary: Endpoint{Model: "flash"}}, `{"candidates":[{"content":{"parts":[{"text":"short"}]}}]}`)

	got, err := c.Summarize(t.Context(), "a long passage", lang.Language{})
	if err != nil || got.Text != "short" {
		t.Fatalf("Summarize = %q, %v", got.Text, err)
	}

	req := <-seen
	if req.SystemInstruction == nil || len(req.SystemInstruction.Parts) == 0 || req.SystemInstruction.Parts[0].Text == "" {
		t.Errorf("no system instruction in %+v", req)
	}
}

// A blocked request is refused for what it says; asking again changes nothing.
func TestGeminiBlockedIsNotRetried(t *testing.T) {
	t.Parallel()

	c, seen := fakeGemini(t, Config{Summary: Endpoint{Model: "flash"}}, `{"promptFeedback":{"blockReason":"SAFETY"}}`)

	if _, err := c.Summarize(t.Context(), "passage", lang.Language{}); err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("err = %v, want the block reason", err)
	}

	if n := len(seen); n != 1 {
		t.Errorf("Gemini was called %d times, want 1", n)
	}
}
