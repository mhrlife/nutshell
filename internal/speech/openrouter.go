// Package speech converts speech to text and text to speech through OpenRouter.
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mhrlife/nutshell/internal/lang"
)

const (
	chatURL   = "https://openrouter.ai/api/v1/chat/completions"
	speechURL = "https://openrouter.ai/api/v1/audio/speech"

	requestTimeout = 3 * time.Minute
	maxErrorBody   = 4 << 10
	maxAudioBody   = 64 << 20
)

// ErrDisabled is returned when no API key was configured.
var ErrDisabled = errors.New("speech: no OpenRouter API key configured")

// Transcript is the result of Transcribe.
type Transcript struct {
	Text    string
	CostUSD float64
}

// Clip is the result of Speak.
type Clip struct {
	Audio        []byte // WAV
	GenerationID string // pass to GenerationCost once OpenRouter has priced the clip
}

// Config selects the models used for each direction.
type Config struct {
	APIKey   string
	STTModel string // chat model with audio input, e.g. google/gemini-3.8-flash
	// SummaryModel is the chat model that shortens a selected passage before
	// it is spoken, e.g. google/gemini-3.8-flash.
	SummaryModel string
	TTSModel     string // text-to-speech model, e.g. x-ai/grok-voice-tts-1.0
	Voice        string // voice name understood by TTSModel
	Style        string // delivery instructions placed before the transcript, see ResolveStyle
	// Speed multiplies how fast the voice talks; 0 and 1 leave the model's
	// own pace. x-ai/grok-voice-tts-1.0 accepts 0.7 to 1.5.
	Speed float64
}

// Client talks to OpenRouter. The zero value is disabled.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for cfg.
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: requestTimeout}}
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c != nil && c.cfg.APIKey != "" }

// Transcribe returns the words spoken in a base64-encoded audio clip.
// format is the container name OpenRouter expects, e.g. "wav" or "mp3".
// l is the language the browser had selected, which tells the model what to
// expect from the microphone.
func (c *Client) Transcribe(ctx context.Context, audioB64, format string, l lang.Language) (Transcript, error) {
	messages := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": TranscribeInstruction(l)},
				map[string]any{
					"type":        "input_audio",
					"input_audio": map[string]string{"data": audioB64, "format": format},
				},
			},
		},
	}

	text, cost, err := c.chat(ctx, c.cfg.STTModel, messages)
	if err != nil {
		return Transcript{}, err
	}

	return Transcript{Text: text, CostUSD: cost}, nil
}

// chat sends one chat completion and returns the reply with what it cost.
func (c *Client) chat(ctx context.Context, model string, messages []any) (string, float64, error) {
	body := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": 0,
		"usage":       map[string]bool{"include": true},
	}

	resp, err := c.post(ctx, chatURL, body)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("decoding reply: %w", err)
	}

	if len(out.Choices) == 0 {
		return "", 0, errors.New("openrouter returned no reply")
	}

	return strings.TrimSpace(out.Choices[0].Message.Content), out.Usage.Cost, nil
}

// Speak converts text to a WAV clip. l is the language the text is written
// in, so the voice reads it the way that language is spoken rather than
// sounding out foreign words.
func (c *Client) Speak(ctx context.Context, text string, l lang.Language) (Clip, error) {
	resp, err := c.post(ctx, speechURL, c.speechRequest(c.speechInput(text, l)))
	if err != nil {
		return Clip{}, err
	}
	defer resp.Body.Close()

	pcm, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBody))
	if err != nil {
		return Clip{}, fmt.Errorf("reading audio: %w", err)
	}

	if len(pcm) == 0 {
		return Clip{}, errors.New("openrouter returned no audio")
	}

	// The audio body carries no usage data; the generation record priced a
	// few seconds later does, see GenerationCost.
	return Clip{
		Audio:        wavFromPCM16(pcm, pcmSampleRate, pcmChannels),
		GenerationID: resp.Header.Get("X-Generation-Id"),
	}, nil
}

// speechInput is what the voice is handed to say text in l: the delivery
// instructions, then the text. The browser marks up what it sends with speech
// tags, so a voice that does not perform them gets the text without, rather
// than reading every tag out.
func (c *Client) speechInput(text string, l lang.Language) string {
	if !speaksTags(c.cfg.TTSModel) {
		text = stripSpeechTags(text)
	}

	return SpeakInstruction(c.cfg.Style, l) + text
}

// speechRequest is the /audio/speech body that reads input aloud.
func (c *Client) speechRequest(input string) map[string]any {
	body := map[string]any{
		"model":           c.cfg.TTSModel,
		"input":           input,
		"voice":           c.cfg.Voice,
		"response_format": "pcm",
	}

	if c.cfg.Speed != 0 && c.cfg.Speed != 1 {
		// OpenRouter documents a top-level speed, but does not hand it on to
		// xAI: x-ai/grok-voice-tts-1.0 only speeds up when the value travels
		// as an xAI provider option. Other providers ignore that option.
		body["speed"] = c.cfg.Speed
		body["provider"] = map[string]any{
			"options": map[string]any{"xai": map[string]any{"speed": c.cfg.Speed}},
		}
	}

	return body
}

func (c *Client) post(ctx context.Context, url string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	return c.do(ctx, http.MethodPost, url, bytes.NewReader(b))
}

func (c *Client) do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "nutshell")

	started := time.Now()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling openrouter: %w", err)
	}

	slog.Debug("openrouter", "url", url, "status", resp.Status, "ms", time.Since(started).Milliseconds())

	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()

		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

		return nil, fmt.Errorf("openrouter %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	return resp, nil
}
