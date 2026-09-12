// Package claudecode implements agent.Agent on top of the Claude Code CLI.
//
// One `claude -p --input-format stream-json` process is kept alive across
// turns so the conversation keeps its history. If the process dies or is
// cancelled, the next turn starts a new one with --resume.
//
// Threads are sessions: one per thread, remembered here by thread name. Only
// one process runs at a time, so a question asked on another thread replaces
// it with one resuming that thread's session — and a side thread's first
// question forks the session of the thread it came from.
//
// The process's stream is read from end to end by one goroutine, not only
// while a question is outstanding: claude answers twice when a background
// task it started finishes, and that second answer belongs to no question.
// turn.go does that reading and says where each turn's events go.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

const (
	stderrTail    = 4 << 10
	maxLineBytes  = 64 << 20
	eventChanSize = 64
)

// Agent drives one Claude Code process.
type Agent struct {
	argv      []string
	launch    Launch
	extraArgs []string
	logger    *slog.Logger

	asking sync.Mutex // serializes Ask calls

	mu        sync.Mutex // guards the fields below
	proc      *process
	unasked   agent.Unasked     // where turns claude took on its own are reported
	language  lang.Language     // the language proc's system prompt was built for
	thread    agent.Thread      // the thread proc is carrying on
	sessions  map[string]string // session id of every thread asked something so far
	cancelled bool
	costBase  float64                       // cumulative cost already attributed to earlier turns of this process
	prompts   map[string]context.CancelFunc // pending questions, by control request id
}

var _ agent.Agent = (*Agent)(nil)

// process is one running claude executable.
type process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdinMu sync.Mutex // answers to control requests are written from many goroutines
	events  <-chan streamEvent
	stderr  *tailBuffer
	stop    context.CancelFunc

	turnMu sync.Mutex    // guards the two fields below
	turn   *turn         // the turn the stream is carrying, nil between turns
	freed  chan struct{} // closed and replaced every time a turn ends
}

// send writes one newline-terminated JSON message to the process.
func (p *process) send(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}

	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()

	_, err = p.stdin.Write(append(line, '\n'))

	return err
}

// New returns an agent that runs argv (normally ["claude"]) in the current
// directory. launch says whether argv is the Claude Code CLI itself or a host
// CLI that starts it. extraArgs are appended to the Claude Code flags
// unchanged, so callers can forward flags such as --model or --mcp-config.
// logger receives what the agent logs.
func New(argv []string, launch Launch, extraArgs []string, logger *slog.Logger) *Agent {
	return &Agent{
		argv: argv, launch: launch, extraArgs: extraArgs, logger: logger,
		sessions: map[string]string{},
	}
}

// Name implements agent.Agent.
func (a *Agent) Name() string {
	if a.launch == WrapperLaunch {
		return "claude code via " + a.argv[0]
	}

	return "claude code"
}

// SessionID returns the Claude Code session id of one thread, once that
// thread has been asked something.
func (a *Agent) SessionID(thread agent.Thread) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.sessions[thread.Name()]
}

// Watch implements agent.Agent.
func (a *Agent) Watch(u agent.Unasked) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.unasked = u
}

// Cancel implements agent.Agent.
func (a *Agent) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelled = true
	a.stopLocked()
}

// Close implements agent.Agent.
func (a *Agent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopLocked()

	return nil
}

func (a *Agent) stopLocked() {
	if a.proc != nil {
		a.proc.stop()
		a.proc = nil
	}
}

// Ask implements agent.Agent.
func (a *Agent) Ask(ctx context.Context, req agent.Request, h agent.Handler) (agent.Answer, error) {
	a.asking.Lock()
	defer a.asking.Unlock()
	defer a.withdrawAllPrompts()

	// A turn claude took on its own may still be running on the process this
	// question is about to use — or to replace. Wait it out: two turns cannot
	// share one stream, and a process killed halfway through one loses it.
	if err := a.settle(ctx); err != nil {
		return agent.Answer{}, err
	}

	proc, err := a.ensureProcess(ctx, req.Language, req.Thread)
	if err != nil {
		return agent.Answer{}, err
	}

	t := newTurn(h)
	if err := proc.claim(ctx, t); err != nil {
		return agent.Answer{}, err
	}

	if err := proc.send(userMessage(req.Message())); err != nil {
		proc.release() // the question never arrived, so this turn never starts

		a.mu.Lock()
		a.stopLocked()
		a.mu.Unlock()

		return agent.Answer{}, fmt.Errorf("sending question to claude: %w", err)
	}

	return a.await(ctx, t)
}

// settle waits for the running process, if there is one, to be between turns.
func (a *Agent) settle(ctx context.Context) error {
	a.mu.Lock()
	proc := a.proc
	a.mu.Unlock()

	if proc == nil {
		return nil
	}

	return proc.idle(ctx)
}

// await blocks until the turn ends: with the answer claude reached, with the
// error that stopped it, or because the user gave up on it.
func (a *Agent) await(ctx context.Context, t *turn) (agent.Answer, error) {
	select {
	case <-ctx.Done():
		a.Cancel()

		return agent.Answer{}, ctx.Err()
	case done := <-t.done:
		return done.answer, done.err
	}
}

// ensureProcess returns the running process, starting one when there is none
// and replacing one started for another language or another thread. Neither
// the language rules, which reach claude through --append-system-prompt, nor
// the conversation to carry on, which reaches it through --resume, can be
// changed once the process is up; both survive the restart, because the new
// process resumes where the old one left off.
func (a *Agent) ensureProcess(ctx context.Context, l lang.Language, t agent.Thread) (*process, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case a.proc == nil:
	case a.language.Code != l.Code:
		a.logger.DebugContext(ctx, "language changed, restarting claude", "from", a.language.Code, "to", l.Code)
		a.stopLocked()
	case a.thread.Name() != t.Name():
		a.logger.DebugContext(ctx, "thread changed, restarting claude", "from", a.thread.Name(), "to", t.Name())
		a.stopLocked()
	}

	a.language = l
	a.thread = t

	if a.proc == nil {
		proc, err := a.startLocked()
		if err != nil {
			return nil, err
		}

		a.proc = proc
		a.cancelled = false
		a.costBase = 0
	}

	return a.proc, nil
}

func (a *Agent) startLocked() (*process, error) {
	argv := a.command()

	ctx, stop := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // running the user's own agent with the flags they passed

	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()

		return nil, fmt.Errorf("claude stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()

		return nil, fmt.Errorf("claude stdout: %w", err)
	}

	stderr := &tailBuffer{max: stderrTail}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		stop()

		return nil, fmt.Errorf("starting claude: %w", err)
	}

	events := make(chan streamEvent, eventChanSize)

	go readEvents(cmd, stdout, events)

	proc := &process{
		cmd: cmd, stdin: stdin, events: events, stderr: stderr, stop: stop,
		freed: make(chan struct{}),
	}

	// ctx ends when the process does, which is what withdraws the questions
	// it left on screen.
	go a.pump(ctx, proc)
	// Announce ourselves as the host that answers control requests. Claude
	// Code carries on without this, so a failure here is not fatal.
	_ = proc.send(map[string]any{
		"type":       "control_request",
		"request_id": "nutshell-initialize",
		"request":    map[string]any{"subtype": "initialize"},
	})

	return proc, nil
}

// readEvents decodes stdout line by line until the process exits, then closes events.
func readEvents(cmd *exec.Cmd, stdout io.Reader, events chan<- streamEvent) {
	defer close(events)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), maxLineBytes)

	for sc.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // claude occasionally prints non-JSON diagnostics
		}

		events <- ev
	}

	_ = cmd.Wait()
}

func userMessage(text string) map[string]any {
	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	}
}

// handleEvent reports progress for one stream event and returns done=true on the final result.
func handleEvent(ev streamEvent, progress func(agent.Event)) (agent.Answer, bool, error) {
	switch ev.Type {
	case typeAssistant:
		for _, b := range parseBlocks(ev.Message) {
			switch b.Type {
			case "tool_use":
				progress(agent.Event{Kind: agent.KindTool, Tool: b.Name, Detail: describeInput(b.Input)})
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" && !strings.Contains(t, "<summary>") {
					progress(agent.Event{Kind: agent.KindText, Text: t})
				}
			}
		}
	case typeResult:
		if ev.IsError {
			return agent.Answer{}, true, fmt.Errorf("claude code: %s", ev.Result)
		}

		answer := agent.ParseAnswer(ev.Result)
		answer.CostUSD = ev.TotalCostUSD // cumulative; awaitResult turns it into a delta
		answer.CostKnown = true

		return answer, true, nil
	}

	return agent.Answer{}, false, nil
}

// chargeTurn converts the cumulative cost reported by claude into this turn's
// share. A turn that reports no total at all — a failed one — leaves the base
// where it was, so what it did spend is charged to the turn after it rather
// than to every turn that follows.
func (a *Agent) chargeTurn(answer *agent.Answer) {
	a.mu.Lock()
	defer a.mu.Unlock()

	total := answer.CostUSD
	if total <= 0 {
		return
	}

	answer.CostUSD = max(total-a.costBase, 0)
	a.costBase = total
}

func (a *Agent) exitError(proc *process) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cancelled := a.cancelled
	if a.proc == proc {
		a.proc = nil
	}

	if cancelled {
		return agent.ErrCancelled
	}

	tail := strings.TrimSpace(proc.stderr.String())
	if tail == "" {
		tail = "no output"
	}

	return errors.New("claude code exited: " + tail)
}

// setSessionID records the session the running process turned out to be.
// It belongs to the thread that process was started for: a forked session is
// a new one, and writing it anywhere else would lose the thread it came from.
func (a *Agent) setSessionID(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sessions[a.thread.Name()] = id
}
