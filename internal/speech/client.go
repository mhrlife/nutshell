// Package speech converts speech to text and text to speech through a speech
// provider's API: the OpenAI interface (OpenRouter by default) or Gemini's.
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
	requestTimeout = 3 * time.Minute
	maxErrorBody   = 4 << 10
	maxAudioBody   = 64 << 20
	// maxEncodedAudio caps a reply that carries audio as base64 before it is
	// decoded: base64 and the JSON around it take more room than the samples.
	maxEncodedAudio = 2 * maxAudioBody

	// PCMSampleRate is the samples per second in the audio every provider is
	// asked for: little-endian 16-bit samples, 24 kHz mono. Raw samples are
	// what let the browser put one clip straight after another and play them
	// as they land: there is no container to open, and no frame that has to
	// be whole before it means anything.
	PCMSampleRate = 24000
	// firstSample is one 16-bit sample: the proof that audio is on its way.
	firstSample = 2
)

// roleUser is the role the user's side of an exchange takes, in both interfaces.
const roleUser = "user"

// The interfaces a provider can speak, named as the configuration file names them.
const (
	InterfaceOpenAI = "openai" // /chat/completions and /audio/speech, see openai.go
	InterfaceGemini = "gemini" // models/{model}:generateContent, see gemini.go
)

// ErrDisabled is returned when the provider for a call has no API key.
var ErrDisabled = errors.New("speech: no API key configured")

// Transcript is the result of Transcribe.
type Transcript struct {
	Text    string
	CostUSD float64
}

// Clip is the result of Speak: the audio of one spoken passage, still being
// spoken while it is read. The caller reads it to the end and closes it.
type Clip struct {
	Audio io.ReadCloser // raw PCM at PCMSampleRate, in the order it is spoken
	// GenerationID is what to pass to GenerationCost once the provider has
	// priced the clip. Only OpenRouter sends one; it is empty elsewhere.
	GenerationID string
}

// Endpoint is a model on one provider's API.
type Endpoint struct {
	Interface string // InterfaceOpenAI or InterfaceGemini; empty means OpenAI
	BaseURL   string // API root, e.g. https://openrouter.ai/api/v1
	APIKey    string
	Model     string
}

// Config selects where each direction runs.
type Config struct {
	STT Endpoint // chat model with audio input, e.g. google/gemini-3.8-flash
	// Transcription is the mode a Gemini transcription model runs in, SMART
	// or VERBATIM. Such a model takes the recording alone, without the
	// instructions a chat model is given; empty means STT is a chat model.
	Transcription string
	// Summary is the chat model that shortens a selected passage before it is
	// spoken, e.g. google/gemini-3.8-flash.
	Summary Endpoint
	TTS     Endpoint // text-to-speech model, e.g. google/gemini-3.1-flash-tts-preview
	// SpeakOverChat reaches an OpenAI-interface TTS model through
	// /chat/completions with audio output instead of /audio/speech: LiteLLM
	// serves Gemini's TTS models only there.
	SpeakOverChat bool
	Voice         string // voice name understood by the TTS model
	Style         string // delivery instructions placed before the transcript, see ResolveStyle
}

// chatRequest is one exchange with a chat model, in no provider's shape yet.
type chatRequest struct {
	system string      // instructions ahead of the exchange; empty for none
	text   string      // the user's words; empty for none
	audio  *audioInput // a recording that goes with them
	// transcription runs a Gemini transcription model in this mode.
	transcription string
}

// audioInput is a recording as the browser sent it.
type audioInput struct {
	data   string // base64
	format string // container name, e.g. wav
}

// Client talks to the speech providers. The zero value is disabled.
type Client struct {
	cfg        Config
	http       *http.Client
	logger     *slog.Logger
	retryDelay time.Duration // the wait before the first retry
}

// New returns a client for cfg that logs to logger.
func New(cfg Config, logger *slog.Logger) *Client {
	return &Client{
		cfg: cfg, http: &http.Client{Timeout: requestTimeout}, logger: logger,
		retryDelay: retryDelay,
	}
}

// Enabled reports whether both directions of voice have an API key.
func (c *Client) Enabled() bool {
	return c != nil && c.cfg.STT.APIKey != "" && c.cfg.TTS.APIKey != ""
}

// Transcribe returns the words spoken in a base64-encoded audio clip.
// format is the container name the browser recorded, e.g. "wav" or "mp3".
// l is the language the browser had selected, which tells a chat model what
// to expect from the microphone.
func (c *Client) Transcribe(ctx context.Context, audioB64, format string, l lang.Language) (Transcript, error) {
	rec := &audioInput{data: audioB64, format: format}

	req := chatRequest{text: TranscribeInstructions(l), audio: rec}
	if c.cfg.Transcription != "" {
		req = chatRequest{audio: rec, transcription: c.cfg.Transcription}
	}

	text, cost, err := c.chat(ctx, c.cfg.STT, req)
	if err != nil {
		return Transcript{}, err
	}

	return Transcript{Text: text, CostUSD: cost}, nil
}

// chat sends one exchange, trying again after a transient failure, and
// returns the reply with what it cost.
func (c *Client) chat(ctx context.Context, ep Endpoint, req chatRequest) (string, float64, error) {
	var (
		reply string
		cost  float64
	)

	err := c.retry(ctx, ep.Model, func() error {
		var err error

		reply, cost, err = c.chatOnce(ctx, ep, req)

		return err
	})

	return reply, cost, err
}

// chatOnce is a single attempt at chat, in the shape ep's interface speaks.
func (c *Client) chatOnce(ctx context.Context, ep Endpoint, req chatRequest) (string, float64, error) {
	if ep.Interface == InterfaceGemini {
		return c.geminiChat(ctx, ep, req)
	}

	return c.openaiChat(ctx, ep, req)
}

// Speak sets the voice reading text and returns the clip while it is still
// being spoken, trying again after a transient failure. l is the language the
// text is written in, so the voice reads it the way that language is spoken
// rather than sounding out foreign words. The caller closes the clip.
func (c *Client) Speak(ctx context.Context, text string, l lang.Language) (Clip, error) {
	input := SpeakInstructions(c.cfg.Style, l) + text

	var clip Clip

	err := c.retry(ctx, c.cfg.TTS.Model, func() error {
		var err error

		clip, err = c.speakOnce(ctx, input)

		return err
	})

	return clip, err
}

// speakOnce is a single attempt at Speak. It waits for the first sample
// before handing the clip on: a provider that accepts the request and then
// says nothing is worth another try, and once the caller is reading the audio
// there is no way back to try anything.
func (c *Client) speakOnce(ctx context.Context, input string) (Clip, error) {
	audio, generationID, err := c.startSpeech(ctx, input)
	if err != nil {
		return Clip{}, err
	}

	first := make([]byte, firstSample)
	if _, err := io.ReadFull(audio, first); err != nil {
		_ = audio.Close()

		return Clip{}, fmt.Errorf("the speech provider returned no audio: %w", err)
	}

	return Clip{
		Audio: cappedBody{
			Reader: io.MultiReader(bytes.NewReader(first), io.LimitReader(audio, maxAudioBody)),
			Closer: audio,
		},
		GenerationID: generationID,
	}, nil
}

// startSpeech asks the TTS provider to read input and returns its raw PCM as
// it arrives, with the generation ID to price it by, if there is one.
func (c *Client) startSpeech(ctx context.Context, input string) (io.ReadCloser, string, error) {
	if c.cfg.TTS.Interface == InterfaceGemini {
		return c.geminiSpeech(ctx, input)
	}

	return c.openaiSpeech(ctx, input)
}

// cappedBody is a response body that stops reading at maxAudioBody, so a
// stream that never ends cannot fill memory.
type cappedBody struct {
	io.Reader
	io.Closer
}

func (c *Client) post(ctx context.Context, ep Endpoint, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	return c.do(ctx, ep, http.MethodPost, path, bytes.NewReader(b))
}

func (c *Client) do(ctx context.Context, ep Endpoint, method, path string, body io.Reader) (*http.Response, error) {
	if c == nil || ep.APIKey == "" {
		return nil, ErrDisabled
	}

	url := strings.TrimRight(ep.BaseURL, "/") + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// The key always goes in a header: a URL ends up in the debug log.
	if ep.Interface == InterfaceGemini {
		req.Header.Set("X-Goog-Api-Key", ep.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+ep.APIKey)
		req.Header.Set("X-Title", "nutshell") // names the app on OpenRouter; others ignore it
	}

	started := time.Now()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling the speech provider: %w", err)
	}

	c.logger.DebugContext(ctx, "speech call",
		"url", url, "status", resp.Status, "ms", time.Since(started).Milliseconds())

	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()

		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

		return nil, &StatusError{Code: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}

	return resp, nil
}
