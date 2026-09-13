package speech

// The OpenAI interface: /chat/completions for transcription and summaries,
// /audio/speech for the voice. OpenRouter speaks it, and adds what a call
// cost on top. A proxy such as LiteLLM serves some TTS models only through
// /chat/completions with audio output, see openaiChatSpeech.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	chatPath   = "/chat/completions"
	speechPath = "/audio/speech"
)

// openaiError is how OpenRouter and LiteLLM report a provider that failed
// after the reply had already gone out as 200.
type openaiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *openaiError) status() error {
	return &StatusError{Code: cmp.Or(e.Code, http.StatusBadGateway), Message: e.Message}
}

// openaiChat is a single chat completion.
func (c *Client) openaiChat(ctx context.Context, ep Endpoint, req chatRequest) (string, float64, error) {
	var messages []any

	if req.system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.system})
	}

	var content []any

	if req.text != "" {
		content = append(content, map[string]any{"type": "text", "text": req.text})
	}

	if req.audio != nil {
		content = append(content, map[string]any{
			"type":        "input_audio",
			"input_audio": map[string]string{"data": req.audio.data, "format": req.audio.format},
		})
	}

	messages = append(messages, map[string]any{"role": roleUser, "content": content})

	body := map[string]any{
		"model":       ep.Model,
		"messages":    messages,
		"temperature": 0,
	}

	resp, err := c.post(ctx, ep, chatPath, body)
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
		// Usage.Cost is OpenRouter's price for the call; other providers
		// leave it out, and the call counts as free.
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
		Error *openaiError `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("decoding reply: %w", err)
	}

	if out.Error != nil {
		return "", 0, out.Error.status()
	}

	if len(out.Choices) == 0 {
		return "", 0, errors.New("the speech provider returned no reply")
	}

	return strings.TrimSpace(out.Choices[0].Message.Content), out.Usage.Cost, nil
}

// openaiSpeech starts the TTS model reading input, through /audio/speech or,
// when the configuration asks for it, through /chat/completions. The audio
// body carries no usage data; OpenRouter's generation record, priced a few
// seconds later, does, see GenerationCost.
func (c *Client) openaiSpeech(ctx context.Context, input string) (io.ReadCloser, string, error) {
	if c.cfg.SpeakOverChat {
		return c.openaiChatSpeech(ctx, input)
	}

	body := map[string]any{
		"model":           c.cfg.TTS.Model,
		"input":           input,
		"voice":           c.cfg.Voice,
		"response_format": "pcm",
	}

	resp, err := c.post(ctx, c.cfg.TTS, speechPath, body)
	if err != nil {
		return nil, "", err
	}

	return resp.Body, resp.Header.Get("X-Generation-Id"), nil
}

// openaiChatSpeech asks /chat/completions for its reply as audio, which is the
// only way LiteLLM serves Gemini's TTS models. The audio comes back whole, as
// base64 inside the JSON, not as a stream: the browser asks for a clip in
// sentence-sized pieces, several at once, so it is only the first piece whose
// words wait for all of its audio.
func (c *Client) openaiChatSpeech(ctx context.Context, input string) (io.ReadCloser, string, error) {
	body := map[string]any{
		"model":      c.cfg.TTS.Model,
		"modalities": []string{"audio"},
		"audio":      map[string]string{"voice": c.cfg.Voice, "format": "pcm16"},
		"messages":   []any{map[string]string{"role": roleUser, "content": input}},
	}

	resp, err := c.post(ctx, c.cfg.TTS, chatPath, body)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	var out struct {
		Choices []struct {
			Message struct {
				Audio *struct {
					Data string `json:"data"` // base64 PCM, 16-bit mono at PCMSampleRate
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
		Error *openaiError `json:"error"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEncodedAudio)).Decode(&out); err != nil {
		return nil, "", fmt.Errorf("decoding reply: %w", err)
	}

	if out.Error != nil {
		return nil, "", out.Error.status()
	}

	if len(out.Choices) == 0 || out.Choices[0].Message.Audio == nil {
		return nil, "", errors.New("the speech provider returned no audio")
	}

	samples, err := base64.StdEncoding.DecodeString(out.Choices[0].Message.Audio.Data)
	if err != nil {
		return nil, "", fmt.Errorf("decoding audio: %w", err)
	}

	return io.NopCloser(bytes.NewReader(samples)), resp.Header.Get("X-Generation-Id"), nil
}
