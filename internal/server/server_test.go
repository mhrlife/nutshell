package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/server"
	"github.com/mhrlife/nutshell/internal/speech"
)

type fakeAgent struct {
	answer  agent.Answer
	err     error
	prompt  agent.Prompt     // when set, Ask puts it to the user before answering
	replies chan agent.Reply // what the user replied
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Cancel()      {}
func (f *fakeAgent) Close() error { return nil }

func (f *fakeAgent) Ask(ctx context.Context, _ string, h agent.Handler) (agent.Answer, error) {
	h.Progress(agent.Event{Kind: agent.KindTool, Tool: "Read", Detail: "a.go"})

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

func (f fakeSpeech) Transcribe(_ context.Context, _, _, _ string) (speech.Transcript, error) {
	return speech.Transcript{Text: "hello", CostUSD: 0.0002}, nil
}

func (f fakeSpeech) Speak(_ context.Context, _ string) (speech.Clip, error) {
	return speech.Clip{Audio: []byte("RIFF"), GenerationID: "gen-1"}, nil
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

func TestAskStreamsTheTurn(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{answer: agent.Answer{Summary: "S", Full: "F"}})
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)
	defer drop()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("ask status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	got := awaitEvent(t, body, `"kind":"result"`)
	for _, want := range []string{`"kind":"question"`, `"text":"hi"`, `"kind":"tool"`, `"name":"Read"`, `"summary":"S"`} {
		if !strings.Contains(got, want) {
			t.Errorf("stream missing %q:\n%s", want, got)
		}
	}
}

func TestAskReportsError(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{err: errors.New("boom")})
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)
	defer drop()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	if got := awaitEvent(t, body, `"kind":"error"`); !strings.Contains(got, "boom") {
		t.Errorf("unexpected stream:\n%s", got)
	}
}

// A page that reloads asks for the whole conversation again; one that only
// dropped its connection asks for what it missed.
func TestStreamReplaysFromWhereTheClientStopped(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{answer: agent.Answer{Summary: "S", Full: "F"}})
	defer ts.Close()

	first, drop := openStream(t, ts.URL, 0)

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	awaitEvent(t, first, `"kind":"result"`)
	drop() // the tab closes

	replay, dropReplay := openStream(t, ts.URL, 0)
	defer dropReplay()

	if got := awaitEvent(t, replay, `"kind":"result"`); !strings.Contains(got, `"kind":"question"`) {
		t.Errorf("the question was not replayed:\n%s", got)
	}

	resumed, dropResumed := openStream(t, ts.URL, 1) // the question is entry 1
	defer dropResumed()

	if got := awaitEvent(t, resumed, `"kind":"result"`); strings.Contains(got, `"kind":"question"`) {
		t.Errorf("resumed stream repeated what the client already had:\n%s", got)
	}
}

func TestAskRejectsEmptyQuestion(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/ask", `{"text":"  "}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAndStatic(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config") //nolint:noctx // test client
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`"agent":"fake"`, `"project":"demo"`, `"voice":true`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("config missing %q: %s", want, body)
		}
	}

	page, err := http.Get(ts.URL + "/") //nolint:noctx // test client
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()

	html, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(html), "<h1>ui</h1>") {
		t.Errorf("index not served: %s", html)
	}
}

func TestTranscribeAndSpeak(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/transcribe", `{"audio":"AAAA","format":"wav","hint":"Persian"}`)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"text":"hello"`) || !strings.Contains(string(body), `"cost_usd":0.0002`) {
		t.Errorf("transcribe: %s", body)
	}

	audio := post(t, ts.URL+"/api/speak", `{"text":"hi"}`)
	defer audio.Body.Close()

	if ct := audio.Header.Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("content type = %q", ct)
	}

	if id := audio.Header.Get("X-Generation-Id"); id != "gen-1" {
		t.Errorf("generation id header = %q", id)
	}

	cost, err := http.Get(ts.URL + "/api/cost?id=gen-1") //nolint:noctx // test client
	if err != nil {
		t.Fatal(err)
	}
	defer cost.Body.Close()

	priced, _ := io.ReadAll(cost.Body)
	if !strings.Contains(string(priced), `"cost_usd":0.001`) {
		t.Errorf("cost: %s", priced)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, ts.URL+"/api/settings", strings.NewReader(`{"lang":"fa"}`))
	if err != nil {
		t.Fatal(err)
	}

	put, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer put.Body.Close()

	if put.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d", put.StatusCode)
	}

	get, err := http.Get(ts.URL + "/api/settings") //nolint:noctx // test client
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()

	body, _ := io.ReadAll(get.Body)
	if string(body) != `{"lang":"fa"}` {
		t.Errorf("GET settings = %s", body)
	}
}

func TestAskPromptsAndDeliversTheAnswer(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{
		answer: agent.Answer{Summary: "S", Full: "F"},
		prompt: agent.Prompt{
			ID:    "p1",
			Kind:  agent.PromptPermission,
			Title: "Bash",
			Questions: []agent.Question{{
				ID:      "decision",
				Options: []agent.Option{{ID: "allow", Label: "Allow once"}, {ID: "deny", Label: "Deny"}},
			}},
		},
		replies: make(chan agent.Reply, 1),
	}

	ts := newTestServer(ag)
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)
	defer drop()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	if got := awaitEvent(t, body, `"kind":"prompt"`); !strings.Contains(got, `"title":"Bash"`) {
		t.Fatalf("prompt missing its title:\n%s", got)
	}

	answer := post(t, ts.URL+"/api/answer", `{"id":"p1","choices":{"decision":["allow"]}}`)
	defer answer.Body.Close()

	if answer.StatusCode != http.StatusNoContent {
		t.Fatalf("answer status = %d, want %d", answer.StatusCode, http.StatusNoContent)
	}

	select {
	case reply := <-ag.replies:
		if reply.First("decision") != "allow" {
			t.Errorf("agent got %+v, want allow", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent never received the answer")
	}
}

func TestAnswerRejectsUnknownPrompt(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/answer", `{"id":"nope","choices":{}}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// The turn belongs to the session, not to the page that started it: closing
// the tab must not throw away work in progress, nor a question waiting on an
// answer.
func TestTurnSurvivesTheBrowserGoingAway(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{
		answer:  agent.Answer{Summary: "S"},
		prompt:  agent.Prompt{ID: "p2", Kind: agent.PromptPermission, Questions: []agent.Question{{ID: "decision"}}},
		replies: make(chan agent.Reply, 1),
	}

	ts := newTestServer(ag)
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	awaitEvent(t, body, `"kind":"prompt"`)
	drop() // the tab closes while the prompt is on screen

	reopened, dropReopened := openStream(t, ts.URL, 0)
	defer dropReopened()

	awaitEvent(t, reopened, `"kind":"prompt"`) // the new page finds it still waiting

	answer := post(t, ts.URL+"/api/answer", `{"id":"p2","choices":{"decision":["allow"]}}`)
	defer answer.Body.Close()

	if answer.StatusCode != http.StatusNoContent {
		t.Fatalf("answer status = %d, want %d", answer.StatusCode, http.StatusNoContent)
	}

	select {
	case reply := <-ag.replies:
		if reply.First("decision") != "allow" {
			t.Errorf("agent got %+v, want allow", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent never received the answer")
	}
}

// openStream connects the way the page does, and hands back a way to drop the
// connection the way a closing tab would.
func openStream(t *testing.T, base string, from int) (io.Reader, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/stream?from=%d", base, from), nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // the caller closes it through the returned func
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	return resp.Body, func() {
		cancel()

		_ = resp.Body.Close()
	}
}

// awaitEvent reads a server-sent-event stream until it carries want.
func awaitEvent(t *testing.T, body io.Reader, want string) string {
	t.Helper()

	read := make(chan string, 1)

	go func() {
		var seen strings.Builder

		buf := make([]byte, 4096)

		for {
			n, err := body.Read(buf)
			seen.Write(buf[:n])

			if strings.Contains(seen.String(), want) || err != nil {
				read <- seen.String()

				return
			}
		}
	}()

	select {
	case got := <-read:
		if !strings.Contains(got, want) {
			t.Fatalf("stream missing %q:\n%s", want, got)
		}

		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q", want)

		return ""
	}
}
