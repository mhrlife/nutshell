// Package claudecode implements agent.Agent on top of the Claude Code CLI.
//
// One `claude -p --input-format stream-json` process is kept alive across
// turns so the conversation keeps its history. If the process dies or is
// cancelled, the next turn starts a new one with --resume.
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

	turn sync.Mutex // serializes Ask calls

	mu        sync.Mutex // guards the fields below
	proc      *process
	language  lang.Language // the language proc's system prompt was built for
	sessionID string
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
	return &Agent{argv: argv, launch: launch, extraArgs: extraArgs, logger: logger}
}

// Name implements agent.Agent.
func (a *Agent) Name() string {
	if a.launch == WrapperLaunch {
		return "claude code via " + a.argv[0]
	}

	return "claude code"
}

// SessionID returns the Claude Code session id once one is known.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.sessionID
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
	a.turn.Lock()
	defer a.turn.Unlock()
	defer a.withdrawAllPrompts()

	proc, err := a.ensureProcess(ctx, req.Language)
	if err != nil {
		return agent.Answer{}, err
	}

	if err := proc.send(userMessage(req.Message())); err != nil {
		a.mu.Lock()
		a.stopLocked()
		a.mu.Unlock()

		return agent.Answer{}, fmt.Errorf("sending question to claude: %w", err)
	}

	return a.awaitResult(ctx, proc, h)
}

// ensureProcess returns the running process, starting one when there is none
// and replacing one started for another language. The language rules reach
// claude through --append-system-prompt, which is only read at startup, so
// switching language means a new process; the conversation survives it
// because the new one resumes the same session.
func (a *Agent) ensureProcess(ctx context.Context, l lang.Language) (*process, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.proc != nil && a.language.Code != l.Code {
		a.logger.DebugContext(ctx, "language changed, restarting claude", "from", a.language.Code, "to", l.Code)
		a.stopLocked()
	}

	a.language = l

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

	proc := &process{cmd: cmd, stdin: stdin, events: events, stderr: stderr, stop: stop}
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

func (a *Agent) awaitResult(ctx context.Context, proc *process, h agent.Handler) (agent.Answer, error) {
	for {
		select {
		case <-ctx.Done():
			a.Cancel()

			return agent.Answer{}, ctx.Err()
		case ev, ok := <-proc.events:
			if !ok {
				return agent.Answer{}, a.exitError(proc)
			}

			if ev.SessionID != "" {
				a.setSessionID(ev.SessionID)
			}

			switch ev.Type {
			case "control_request":
				go a.serveControl(ctx, proc, ev, h)

				continue
			case "control_cancel_request":
				a.withdrawPrompt(ev.RequestID)

				continue
			}

			if answer, done, err := handleEvent(ev, h.Progress); done {
				a.chargeTurn(&answer)

				return answer, err
			}
		}
	}
}

// handleEvent reports progress for one stream event and returns done=true on the final result.
func handleEvent(ev streamEvent, progress func(agent.Event)) (agent.Answer, bool, error) {
	switch ev.Type {
	case "assistant":
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
	case "result":
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

// chargeTurn converts the cumulative cost reported by claude into this turn's share.
func (a *Agent) chargeTurn(answer *agent.Answer) {
	a.mu.Lock()
	defer a.mu.Unlock()

	total := answer.CostUSD
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

func (a *Agent) setSessionID(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sessionID = id
}
