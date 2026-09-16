package codex

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

func testAgent(t *testing.T) *Agent {
	t.Helper()

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	a := New(bin, nil, slog.New(slog.DiscardHandler))
	a.argv = []string{bin, "-test.run=^TestCodexProcess$", "--", "nutshell-codex-helper"}

	t.Cleanup(func() { _ = a.Close() })

	return a
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func TestAskThreadsAndLanguage(t *testing.T) {
	t.Parallel()
	a := testAgent(t)

	cases := []struct {
		name     string
		thread   agent.Thread
		language string
		want     string
	}{
		{"new", agent.Thread{}, "en", "thread/start:root"},
		{"resume", agent.Thread{}, "fa", "thread/resume:root"},
		{"fork", agent.Thread{ID: testSide, Parent: "root"}, "en", "thread/fork:side"},
		{"return", agent.Thread{}, "en", "thread/resume:root"},
		{"side resume", agent.Thread{ID: testSide, Parent: "root"}, "fa", "thread/resume:side"},
	}
	for _, tc := range cases { //nolint:paralleltest // these are sequential turns on a shared agent
		t.Run(tc.name, func(t *testing.T) {
			req := agent.Request{
				Text: "answer", Thread: tc.thread, Language: lang.Lookup(tc.language), Selection: "selected passage",
				Notes: []agent.Note{{Title: "topic", Conclusion: "settled"}},
			}

			var events []agent.Event

			answer, err := a.Ask(testContext(t), req, agent.ProgressFunc(func(e agent.Event) { events = append(events, e) }))
			if err != nil {
				t.Fatal(err)
			}

			if answer.Summary != tc.want || answer.CostKnown {
				t.Fatalf("answer = %+v", answer)
			}

			for _, want := range []string{req.Message(), agent.Instructions(req.Language, agent.Structured)} {
				if !strings.Contains(answer.Full, want) {
					t.Errorf("full answer missing %q", want)
				}
			}

			if len(events) != 2 || events[0].Tool != "commandExecution" || events[1].Text != "Working." {
				t.Fatalf("events = %+v", events)
			}
		})
	}
}

func TestAskFailures(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"failed", "invalid", "empty", "exit", "bad-json", "rpc-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			a := testAgent(t)

			_, err := a.Ask(testContext(t), agent.Request{Text: mode}, agent.ProgressFunc(func(agent.Event) {}))
			if err == nil {
				t.Fatal("expected failure")
			}

			if _, err := a.Ask(testContext(t), agent.Request{Text: "answer"}, agent.ProgressFunc(func(agent.Event) {})); err != nil {
				t.Fatalf("recovery: %v", err)
			}
		})
	}
}

type testHandler struct {
	progress func(agent.Event)
	prompt   func(context.Context, agent.Prompt) (agent.Reply, error)
}

func (h testHandler) Progress(e agent.Event) {
	if h.progress != nil {
		h.progress(e)
	}
}

func (h testHandler) Prompt(ctx context.Context, p agent.Prompt) (agent.Reply, error) {
	return h.prompt(ctx, p)
}

func TestAskPrompts(t *testing.T) {
	t.Parallel()
	a := testAgent(t)

	var prompts []agent.Prompt

	h := testHandler{prompt: func(_ context.Context, p agent.Prompt) (agent.Reply, error) {
		prompts = append(prompts, p)
		if p.Kind == agent.PromptChoice {
			return agent.Reply{Choices: map[string][]string{"q": {"custom answer"}}}, nil
		}

		return agent.Reply{Choices: map[string][]string{decisionKey: {agent.OptionAllow}}}, nil
	}}

	answer, err := a.Ask(testContext(t), agent.Request{Text: "prompts"}, h)
	if err != nil {
		t.Fatal(err)
	}

	if len(prompts) != 2 || prompts[0].Kind != agent.PromptPermission || !prompts[1].Questions[0].FreeText || answer.Summary == "" {
		t.Fatalf("prompts = %+v; answer = %+v", prompts, answer)
	}
}

func TestCancelResumesHistory(t *testing.T) {
	t.Parallel()
	a := testAgent(t)
	h := testHandler{progress: func(agent.Event) { a.Cancel() }}

	_, err := a.Ask(testContext(t), agent.Request{Text: "wait"}, h)
	if !errors.Is(err, agent.ErrCancelled) {
		t.Fatalf("cancel: %v", err)
	}

	answer, err := a.Ask(testContext(t), agent.Request{Text: "answer"}, agent.ProgressFunc(func(agent.Event) {}))
	if err != nil || answer.Summary != "thread/resume:root" {
		t.Fatalf("resume = %+v, %v", answer, err)
	}
}

func TestPromptWithdrawn(t *testing.T) {
	t.Parallel()
	a := testAgent(t)
	withdrawn := make(chan struct{})
	h := testHandler{prompt: func(ctx context.Context, _ agent.Prompt) (agent.Reply, error) {
		<-ctx.Done()
		close(withdrawn)

		return agent.Reply{}, ctx.Err()
	}}

	_, err := a.Ask(testContext(t), agent.Request{Text: testWithdraw}, h)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-withdrawn:
	default:
		t.Fatal("prompt worker did not exit")
	}
}

func TestCloseAndMissingParent(t *testing.T) {
	t.Parallel()
	a := testAgent(t)

	_, err := a.Ask(testContext(t), agent.Request{Thread: agent.Thread{ID: testSide, Parent: "missing"}}, agent.ProgressFunc(func(agent.Event) {}))
	if err == nil || !strings.Contains(err.Error(), "parent thread") {
		t.Fatalf("missing parent: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = a.Ask(testContext(t), agent.Request{}, agent.ProgressFunc(func(agent.Event) {}))
	if err == nil {
		t.Fatal("closed agent accepted a turn")
	}
}

func TestAskParentCancellation(t *testing.T) {
	t.Parallel()
	a := testAgent(t)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	h := testHandler{progress: func(agent.Event) { cancel() }}

	_, err := a.Ask(ctx, agent.Request{Text: "wait"}, h)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation: %v", err)
	}
}
