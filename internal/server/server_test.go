package server_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
)

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

// The language is chosen in the browser and travels with the question, so
// the agent can be told the rules of that language and no other.
func TestAskCarriesTheBrowsersLanguage(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "S"}, asked: make(chan agent.Request, 1)}

	ts := newTestServer(ag)
	defer ts.Close()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi","lang":"fa"}`)
	defer resp.Body.Close()

	select {
	case req := <-ag.asked:
		if req.Language.Code != "fa" || !req.Language.Known() {
			t.Errorf("agent got language %+v, want the Persian rules", req.Language)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was never asked")
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

	resp := post(t, ts.URL+"/api/transcribe", `{"audio":"AAAA","format":"wav","lang":"fa"}`)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"text":"hello fa"`) || !strings.Contains(string(body), `"cost_usd":0.0002`) {
		t.Errorf("transcribe: %s", body)
	}

	audio := post(t, ts.URL+"/api/speak", `{"text":"hi","lang":"fa"}`)
	defer audio.Body.Close()

	if ct := audio.Header.Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("content type = %q", ct)
	}

	if clip, _ := io.ReadAll(audio.Body); string(clip) != "RIFF fa" {
		t.Errorf("the voice was not told the language: %s", clip)
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

// Once OpenRouter has failed every retry, the page is told it was the
// connection, so it keeps the recording and offers to send it again.
func TestTranscribeReportsAnUnreachableProvider(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/transcribe", `{"audio":"unavailable","format":"wav"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
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
