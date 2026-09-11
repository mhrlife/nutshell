package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
)

// A question about a selected passage reaches the agent with the whole
// passage, and the transcript keeps the excerpt the agent was actually shown.
func TestAskCarriesTheSelection(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "S"}, asked: make(chan agent.Request, 1)}

	ts := newTestServer(ag)
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)
	defer drop()

	passage := strings.Repeat("a", 300) + strings.Repeat("b", 300)
	payload, _ := json.Marshal(map[string]string{"text": "why?", "selection": passage})

	resp := post(t, ts.URL+"/api/ask", string(payload))
	defer resp.Body.Close()

	select {
	case req := <-ag.asked:
		if req.Selection != passage || req.Text != "why?" {
			t.Errorf("agent got text %q and selection %q", req.Text, req.Selection)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was never asked")
	}

	got := awaitEvent(t, body, `"kind":"result"`)
	if want := `"selection":` + mustJSON(t, agent.Excerpt(passage)); !strings.Contains(got, want) {
		t.Errorf("question entry missing the excerpt %s:\n%s", want, got)
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/summarize", `{"text":"a long passage","lang":"fa"}`)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"text":"fa summary of a long passage"`) ||
		!strings.Contains(string(body), `"speech":"[pause] fa summary of a long passage"`) ||
		!strings.Contains(string(body), `"cost_usd":0.0003`) {
		t.Errorf("summarize: %s", body)
	}

	empty := post(t, ts.URL+"/api/summarize", `{"text":"  "}`)
	defer empty.Body.Close()

	if empty.StatusCode != http.StatusBadRequest {
		t.Errorf("empty passage status = %d, want 400", empty.StatusCode)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
