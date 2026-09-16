// Package codex implements agent.Agent using the Codex app-server protocol.
// Each Ask opens a connection and resumes the thread's stored history. This
// lets language instructions change between turns without replaying messages.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
)

// Agent drives Codex in the current working directory.
type Agent struct {
	argv     []string
	logger   *slog.Logger
	asking   sync.Mutex
	sessions map[string]conversation // guarded by asking
	mu       sync.Mutex
	cancel   context.CancelFunc
	closed   bool
}

var _ agent.Agent = (*Agent)(nil)

// New returns an agent using bin and app-server arguments. Codex's own
// configuration supplies the model, authentication and sandbox policy.
func New(bin string, args []string, logger *slog.Logger) *Agent {
	return &Agent{
		argv: slices.Concat([]string{
			bin, "app-server",
			"-c", `approval_policy="on-request"`,
			"-c", `approvals_reviewer="auto_review"`,
		}, args, []string{"--listen", "stdio://"}),
		logger: logger, sessions: map[string]conversation{},
	}
}

// Name implements agent.Agent.
func (*Agent) Name() string { return "codex" }

// Watch implements agent.Agent. This adapter runs explicit user turns only.
func (*Agent) Watch(agent.Unasked) {}

// Cancel interrupts the active turn, leaving its history available to Ask.
func (a *Agent) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
}

// Close cancels active work and prevents new turns.
func (a *Agent) Close() error {
	a.mu.Lock()

	a.closed = true
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	a.asking.Lock()
	defer a.asking.Unlock()

	return nil
}

// Ask implements agent.Agent.
func (a *Agent) Ask(ctx context.Context, req agent.Request, h agent.Handler) (agent.Answer, error) {
	a.asking.Lock()
	defer a.asking.Unlock()

	if err := ctx.Err(); err != nil {
		return agent.Answer{}, err
	}

	parent := ctx

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return agent.Answer{}, errors.New("codex agent is closed")
	}

	a.cancel = cancel

	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.cancel = nil; a.mu.Unlock() }()

	// Cancellation first uses turn/interrupt. The process is killed only
	// after the bounded cleanup window, so Codex can persist the interruption.
	p, err := startProcess(context.WithoutCancel(ctx), a.argv)
	if err != nil {
		return agent.Answer{}, err
	}
	defer p.close()

	r := newRun(p, h, a.logger)
	defer r.close()

	a.logger.DebugContext(ctx, "starting codex turn", "thread", req.Thread.Name())

	answer, err := a.ask(ctx, req, r)
	if ctx.Err() != nil {
		r.interrupt(context.WithoutCancel(ctx))

		if parent.Err() != nil {
			return agent.Answer{}, parent.Err()
		}

		return agent.Answer{}, agent.ErrCancelled
	}

	return answer, err
}

func (a *Agent) ask(ctx context.Context, req agent.Request, r *run) (agent.Answer, error) {
	setup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := r.call(setup, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "nutshell", "version": "0.1.0"},
		"capabilities": map[string]bool{"experimentalApi": true},
	})
	if err != nil {
		return agent.Answer{}, err
	}

	if err := r.proc.send(map[string]string{keyMethod: "initialized"}); err != nil {
		return agent.Answer{}, err
	}

	r.threadID, err = a.openThread(setup, req, r)
	if err != nil {
		return agent.Answer{}, err
	}

	// A completed turn has persisted history. Until then, keep the mapping
	// provisional so a cancellation before the first turn can recover.
	defer func() {
		if r.finished {
			a.sessions[req.Thread.Name()] = conversation{id: r.threadID}
		}
	}()

	cancel()

	result, err := r.call(ctx, "turn/start", map[string]any{
		keyThreadID:    r.threadID,
		"input":        []map[string]string{{keyType: keyText, keyText: req.Message()}},
		"outputSchema": json.RawMessage(agent.AnswerSchema),
	})
	if err != nil {
		return agent.Answer{}, err
	}

	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &started); err != nil {
		return agent.Answer{}, fmt.Errorf("codex turn: %w", err)
	}

	if started.Turn.ID == "" {
		return agent.Answer{}, errors.New("codex returned no turn id")
	}

	r.turnID = started.Turn.ID
	for !r.finished {
		m, err := r.next(ctx)
		if err != nil {
			return agent.Answer{}, err
		}

		if err := r.handle(ctx, m); err != nil {
			return agent.Answer{}, err
		}
	}

	return r.answer()
}
