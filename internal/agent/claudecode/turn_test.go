package claudecode

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
)

// watcher collects the turns the agent took without being asked.
type watcher struct {
	mu      sync.Mutex
	opened  []agent.Thread
	answers []agent.Answer
	errs    []error
	begun   chan struct{}
	done    chan struct{}
}

func newWatcher() *watcher {
	return &watcher{begun: make(chan struct{}, 4), done: make(chan struct{}, 4)}
}

func (w *watcher) Begin(thread agent.Thread) (agent.Handler, func(agent.Answer, error)) {
	w.mu.Lock()
	w.opened = append(w.opened, thread)
	w.mu.Unlock()

	w.begun <- struct{}{}

	return agent.ProgressFunc(func(agent.Event) {}), func(answer agent.Answer, err error) {
		w.mu.Lock()
		w.answers = append(w.answers, answer)
		w.errs = append(w.errs, err)
		w.mu.Unlock()

		w.done <- struct{}{}
	}
}

// awaitBegun waits for a turn of the agent's own to have been opened. Events
// are buffered on their way to the reader, so a test that wants to act on a
// turn already under way has to know that the reader has got there.
func (w *watcher) awaitBegun(t *testing.T) {
	t.Helper()

	select {
	case <-w.begun:
	case <-time.After(2 * time.Second):
		t.Fatal("the agent never opened a turn of its own")
	}
}

func (w *watcher) await(t *testing.T) (agent.Answer, error) {
	t.Helper()

	select {
	case <-w.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the agent never reported a turn of its own")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.answers[len(w.answers)-1], w.errs[len(w.errs)-1]
}

// scriptedProcess is a process whose stream the test writes by hand, with no
// claude behind it. Nothing is ever sent to it, so stdin goes nowhere.
func scriptedProcess() (*process, chan streamEvent) {
	events := make(chan streamEvent, 16)
	proc := &process{
		stdin:  nopCloser{io.Discard},
		events: events,
		stderr: &tailBuffer{max: stderrTail},
		stop:   func() {},
		freed:  make(chan struct{}),
	}

	return proc, events
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func answerEvents(text string, cost float64) []streamEvent {
	return []streamEvent{
		{Type: typeSystem, Subtype: subtypeInit, SessionID: rootSession},
		{Type: typeAssistant, Message: json.RawMessage(`{"content":[{"type":"tool_use","name":"Bash","input":{"command":"make"}}]}`)},
		{Type: typeResult, Result: "<summary>" + text + "</summary><full>" + text + "</full>", TotalCostUSD: cost},
	}
}

// The bug: claude answers twice when a background task it started finishes,
// and the reader stopped at the first answer. The second one stayed in the
// stream, to be handed out as the answer to whatever was asked next.
func TestStreamKeepsReadingAfterTheAnswer(t *testing.T) {
	t.Parallel()

	a := New([]string{claudeBin}, DirectLaunch, nil, slog.New(slog.DiscardHandler))
	seen := newWatcher()
	a.Watch(seen)
	a.thread = agent.Thread{ID: "t1", Parent: agent.RootThread}

	proc, events := scriptedProcess()

	go a.pump(context.Background(), proc)

	// The turn someone asked for.
	asked := newTurn(agent.ProgressFunc(func(agent.Event) {}))
	if err := proc.claim(context.Background(), asked); err != nil {
		t.Fatalf("claiming the stream: %v", err)
	}

	for _, ev := range answerEvents("the task is running", 0.010) {
		events <- ev
	}

	answer, err := a.await(context.Background(), asked)
	if err != nil || answer.Summary != "the task is running" {
		t.Fatalf("the asked turn answered %+v, %v", answer, err)
	}

	// The turn claude takes on its own once the task finishes.
	for _, ev := range answerEvents("the task is done", 0.025) {
		events <- ev
	}

	own, err := seen.await(t)
	if err != nil || own.Summary != "the task is done" {
		t.Fatalf("the turn claude took on its own reported %+v, %v", own, err)
	}

	if own.CostUSD < 0.0149 || own.CostUSD > 0.0151 {
		t.Errorf("it was charged %v, want the 0.015 it added to the total", own.CostUSD)
	}

	seen.mu.Lock()
	defer seen.mu.Unlock()

	if len(seen.opened) != 1 || seen.opened[0].ID != "t1" {
		t.Errorf("the turn was opened on %+v, want the thread the process is carrying", seen.opened)
	}
}

// A question asked while claude is busy with a turn of its own waits for it,
// rather than being handed that turn's answer.
func TestQuestionWaitsForATurnClaudeTookOnItsOwn(t *testing.T) {
	t.Parallel()

	a := New([]string{claudeBin}, DirectLaunch, nil, slog.New(slog.DiscardHandler))
	seen := newWatcher()
	a.Watch(seen)

	proc, events := scriptedProcess()

	go a.pump(context.Background(), proc)

	// claude starts a turn of its own, and stops halfway through it.
	events <- streamEvent{Type: typeSystem, Subtype: subtypeInit}

	events <- streamEvent{Type: typeAssistant, Message: json.RawMessage(`{"content":[{"type":"text","text":"the task finished"}]}`)}

	seen.awaitBegun(t)

	claimed := make(chan error, 1)
	asked := newTurn(agent.ProgressFunc(func(agent.Event) {}))

	go func() { claimed <- proc.claim(context.Background(), asked) }()

	select {
	case <-claimed:
		t.Fatal("a question took the stream over in the middle of a turn")
	case <-time.After(50 * time.Millisecond):
	}

	events <- streamEvent{Type: typeResult, Result: "<summary>done</summary>", TotalCostUSD: 0.01}

	if own, err := seen.await(t); err != nil || own.Summary != "done" {
		t.Fatalf("the turn claude took on its own reported %+v, %v", own, err)
	}

	select {
	case err := <-claimed:
		if err != nil {
			t.Fatalf("claiming the stream once it was free: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the question never got the stream")
	}
}

// A process that dies mid-turn ends that turn, so nobody waits for an answer
// that is never coming — whether a question was asked or not.
func TestDeadProcessEndsTheTurnItWasCarrying(t *testing.T) {
	t.Parallel()

	a := New([]string{claudeBin}, DirectLaunch, nil, slog.New(slog.DiscardHandler))
	seen := newWatcher()
	a.Watch(seen)

	proc, events := scriptedProcess()

	go a.pump(context.Background(), proc)

	events <- streamEvent{Type: typeSystem, Subtype: subtypeInit}

	close(events)

	if _, err := seen.await(t); err == nil {
		t.Error("the turn ended with no error, though the process died under it")
	}
}
