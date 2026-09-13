package speech

// Gemini's own API: models/{model}:generateContent for transcription and
// summaries, and :streamGenerateContent for the voice, whose audio arrives as
// base64 PCM inside server-sent events. Gemini reports the tokens a call used
// but not its price, so its calls count as free.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const (
	geminiGenerate = ":generateContent"
	geminiStream   = ":streamGenerateContent?alt=sse"
)

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *geminiBlob `json:"inlineData,omitempty"`
	// AudioTranscription is where a transcription model such as
	// gemini-3.5-transcribe puts its transcript, instead of Text.
	AudioTranscription *struct {
		Text string `json:"text"`
	} `json:"audioTranscription,omitempty"`
}

type geminiBlob struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"` // base64
}

// geminiReply is a generateContent response, or one event of a stream.
type geminiReply struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// geminiPath is the API path that runs method on model.
func geminiPath(model, method string) string {
	return "/models/" + strings.TrimPrefix(model, "models/") + method
}

// geminiChat is a single generateContent call. A transcription request sends
// the recording alone and asks for the mode instead of a temperature.
func (c *Client) geminiChat(ctx context.Context, ep Endpoint, req chatRequest) (string, float64, error) {
	var parts []geminiPart

	if req.text != "" {
		parts = append(parts, geminiPart{Text: req.text})
	}

	if req.audio != nil {
		parts = append(parts, geminiPart{InlineData: &geminiBlob{MIMEType: "audio/" + req.audio.format, Data: req.audio.data}})
	}

	generation := map[string]any{"temperature": 0}
	if req.transcription != "" {
		generation = map[string]any{"audioTranscriptionConfig": map[string]string{"mode": req.transcription}}
	}

	body := map[string]any{
		"contents":         []geminiContent{{Role: roleUser, Parts: parts}},
		"generationConfig": generation,
	}

	if req.system != "" {
		body["systemInstruction"] = geminiContent{Parts: []geminiPart{{Text: req.system}}}
	}

	resp, err := c.post(ctx, ep, geminiPath(ep.Model, geminiGenerate), body)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var out geminiReply
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("decoding reply: %w", err)
	}

	text, err := out.text()

	return text, 0, err
}

// text joins the text of the first candidate: its text parts, or the
// transcript a transcription model returns.
func (r geminiReply) text() (string, error) {
	if err := r.failure(); err != nil {
		return "", err
	}

	if len(r.Candidates) == 0 {
		return "", errors.New("gemini returned no reply")
	}

	var b strings.Builder
	for _, p := range r.Candidates[0].Content.Parts {
		b.WriteString(p.Text)

		if p.AudioTranscription != nil {
			b.WriteString(p.AudioTranscription.Text)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

// failure is the error a reply reports in its body, if any. A blocked request
// is refused for what it says, which another try will not change.
func (r geminiReply) failure() error {
	if r.Error != nil {
		return &StatusError{Code: r.Error.Code, Message: r.Error.Message}
	}

	if r.PromptFeedback != nil && r.PromptFeedback.BlockReason != "" {
		return &StatusError{Code: http.StatusUnprocessableEntity, Message: "gemini blocked the request: " + r.PromptFeedback.BlockReason}
	}

	return nil
}

// geminiSpeech starts streamGenerateContent reading input aloud.
func (c *Client) geminiSpeech(ctx context.Context, input string) (io.ReadCloser, string, error) {
	generation := map[string]any{"responseModalities": []string{"AUDIO"}}
	if c.cfg.Voice != "" {
		generation["speechConfig"] = map[string]any{
			"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]string{"voiceName": c.cfg.Voice}},
		}
	}

	body := map[string]any{
		"contents":         []geminiContent{{Role: roleUser, Parts: []geminiPart{{Text: input}}}},
		"generationConfig": generation,
	}

	resp, err := c.post(ctx, c.cfg.TTS, geminiPath(c.cfg.TTS.Model, geminiStream), body) //nolint:bodyclose // geminiAudio closes it
	if err != nil {
		return nil, "", err
	}

	return &geminiAudio{
		body:   resp.Body,
		events: bufio.NewReader(io.LimitReader(resp.Body, maxEncodedAudio)),
	}, "", nil
}

// geminiAudio reads the samples out of a speech stream as its events arrive.
type geminiAudio struct {
	body    io.Closer
	events  *bufio.Reader
	pending []byte // decoded samples not read yet
}

func (a *geminiAudio) Read(p []byte) (int, error) {
	for len(a.pending) == 0 {
		if err := a.nextLine(); err != nil {
			return 0, err
		}
	}

	n := copy(p, a.pending)
	a.pending = a.pending[n:]

	return n, nil
}

func (a *geminiAudio) Close() error { return a.body.Close() }

// nextLine reads one line of the stream and keeps the audio a data line
// carries. Blank lines and anything else in the stream are skipped.
func (a *geminiAudio) nextLine() error {
	line, readErr := a.events.ReadString('\n')

	data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
	if !ok {
		return readErr // nil keeps reading; io.EOF ends the clip
	}

	var event geminiReply
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return fmt.Errorf("decoding gemini audio: %w", err)
	}

	if err := event.failure(); err != nil {
		return err
	}

	for _, cand := range event.Candidates {
		if err := a.keep(cand.Content.Parts); err != nil {
			return err
		}
	}

	return nil // a last line without a newline ends in io.EOF, which the next read returns
}

// keep decodes the audio among parts onto what is waiting to be read.
func (a *geminiAudio) keep(parts []geminiPart) error {
	for _, part := range parts {
		if part.InlineData == nil {
			continue
		}

		if err := checkPCM(part.InlineData.MIMEType); err != nil {
			return err
		}

		samples, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
		if err != nil {
			return fmt.Errorf("decoding gemini audio: %w", err)
		}

		a.pending = append(a.pending, samples...)
	}

	return nil
}

// checkPCM accepts the one format the browser plays: 16-bit mono PCM at
// PCMSampleRate, which Gemini names audio/L16;codec=pcm;rate=24000 or
// audio/l16; rate=24000; channels=1.
func checkPCM(mimeType string) error {
	kind, params, err := mime.ParseMediaType(mimeType)
	if err != nil || kind != "audio/l16" {
		return fmt.Errorf("gemini sent %q audio, not 16-bit PCM", mimeType)
	}

	if rate, ok := params["rate"]; ok && rate != strconv.Itoa(PCMSampleRate) {
		return fmt.Errorf("gemini sent audio at %s Hz, not %d", rate, PCMSampleRate)
	}

	if channels, ok := params["channels"]; ok && channels != "1" {
		return fmt.Errorf("gemini sent audio with %s channels, not mono", channels)
	}

	return nil
}
