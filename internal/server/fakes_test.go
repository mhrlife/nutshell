// The fakes the server tests run against: an agent that answers on command,
// a settings store in memory, and a speech provider that echoes back what it
// was told.
package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
	"github.com/mhrlife/nutshell/internal/server"
	"github.com/mhrlife/nutshell/internal/speech"
)

type fakeAgent struct {
	answer  agent.Answer
	err     error
	panics  string             // when set, Ask panics with it
	prompt  agent.Prompt       // when set, Ask puts it to the user before answering
	replies chan agent.Reply   // what the user replied
	asked   chan agent.Request // the request the turn arrived with
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Cancel()      {}
func (f *fakeAgent) Close() error { return nil }

func (f *fakeAgent) Ask(ctx context.Context, req agent.Request, h agent.Handler) (agent.Answer, error) {
	if f.asked != nil {
		f.asked <- req
	}

	h.Progress(agent.Event{Kind: agent.KindTool, Tool: "Read", Detail: "a.go"})

	if f.panics != "" {
		panic(f.panics)
	}

	if f.prompt.ID != "" {
		reply, err := h.Prompt(ctx, f.prompt)
		if err != nil {
			return agent.Answer{}, err
		}

		f.replies <- reply
	}

	return f.answer, f.err
}

type fakeSettings struct{ doc json.RawMessage }

func (f *fakeSettings) Load() (json.RawMessage, error) {
	if f.doc == nil {
		return json.RawMessage("{}"), nil
	}

	return f.doc, nil
}

func (f *fakeSettings) Save(doc json.RawMessage) error {
	f.doc = doc

	return nil
}

type fakeSpeech struct{ enabled bool }

func (f fakeSpeech) Enabled() bool { return f.enabled }

// Transcribe and Speak echo the language they were handed, which is how the
// tests see that the browser's choice travelled the whole way.
func (f fakeSpeech) Transcribe(_ context.Context, _, _ string, l lang.Language) (speech.Transcript, error) {
	return speech.Transcript{Text: "hello " + l.Code, CostUSD: 0.0002}, nil
}

func (f fakeSpeech) Speak(_ context.Context, _ string, l lang.Language) (speech.Clip, error) {
	return speech.Clip{Audio: []byte("RIFF " + l.Code), GenerationID: "gen-1"}, nil
}

func (f fakeSpeech) Summarize(_ context.Context, passage string, l lang.Language) (speech.Summary, error) {
	text := l.Code + " summary of " + passage

	return speech.Summary{Text: text, Speech: "[pause] " + text, CostUSD: 0.0003}, nil
}

func (f fakeSpeech) GenerationCost(_ context.Context, id string) (float64, error) {
	if id != "gen-1" {
		return 0, errors.New("unknown generation")
	}

	return 0.001, nil
}

func newTestServer(ag agent.Agent) *httptest.Server {
	static := http.FS(fstest.MapFS{"index.html": {Data: []byte("<h1>ui</h1>")}})
	srv := server.New(ag, fakeSpeech{enabled: true}, &fakeSettings{}, static, server.Config{Lang: "en", Project: "demo"})

	return httptest.NewServer(srv)
}

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	return resp
}
