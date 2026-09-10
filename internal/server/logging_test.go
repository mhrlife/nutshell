package server_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// captureLog points the default logger at a buffer for the duration of a test.
// The tests using it do not run in parallel: they read what everyone wrote.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()

	buf := &syncBuffer{}
	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return buf
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// The browser is the half of nutshell the terminal cannot see, so it reports
// its own failures.
//
//nolint:paralleltest // captures the default logger, which is process-wide
func TestClientLogReachesTheTerminal(t *testing.T) {
	log := captureLog(t)

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/log", `{"level":"error","event":"transcribe","message":"HTTP 502","detail":"openrouter 429"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	for _, want := range []string{"level=ERROR", "browser", "transcribe", "HTTP 502", "openrouter 429"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log missing %q:\n%s", want, log.String())
		}
	}
}

// A request that fails leaves a line behind even when nobody is watching the
// page it failed on.
//
//nolint:paralleltest // captures the default logger, which is process-wide
func TestFailedRequestIsLogged(t *testing.T) {
	log := captureLog(t)

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/ask", `{"text":"   "}`)
	defer resp.Body.Close()

	for _, want := range []string{"request failed", "/api/ask", "status=400", "empty question"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log missing %q:\n%s", want, log.String())
		}
	}
}

// A panicking agent used to take the process with it and leave the page
// waiting for ever; now the turn just fails.
//
//nolint:paralleltest // captures the default logger, which is process-wide
func TestTurnSurvivesAPanickingAgent(t *testing.T) {
	log := captureLog(t)

	ts := newTestServer(&fakeAgent{panics: "kaboom"})
	defer ts.Close()

	body, drop := openStream(t, ts.URL, 0)
	defer drop()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi"}`)
	defer resp.Body.Close()

	if got := awaitEvent(t, body, `"kind":"error"`); !strings.Contains(got, "kaboom") {
		t.Errorf("the panic never reached the page:\n%s", got)
	}

	if !strings.Contains(log.String(), "turn panicked") {
		t.Errorf("the panic was not logged:\n%s", log.String())
	}

	// The agent is free again: the next question is accepted.
	next := post(t, ts.URL+"/api/ask", `{"text":"again"}`)
	defer next.Body.Close()

	if next.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", next.StatusCode, http.StatusAccepted)
	}

	_, _ = io.Copy(io.Discard, next.Body)
}
