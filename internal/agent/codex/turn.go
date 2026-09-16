package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
)

type run struct {
	proc             *process
	handler          agent.Handler
	logger           *slog.Logger
	nextID           int
	promptPrefix     string
	threadID, turnID string
	final, fallback  string
	finished         bool
	turnErr          error
	prompts          map[string]context.CancelFunc
	workers          sync.WaitGroup
	items            map[string]string // file change descriptions for approval requests
}

func newRun(p *process, h agent.Handler, logger *slog.Logger) *run {
	return &run{promptPrefix: "codex:" + rand.Text() + ":", proc: p, handler: h, logger: logger, prompts: map[string]context.CancelFunc{}, items: map[string]string{}}
}

func (r *run) close() {
	for _, cancel := range r.prompts {
		cancel()
	}

	r.proc.stop()
	r.workers.Wait()
}

func (r *run) next(ctx context.Context) (message, error) {
	select {
	case <-ctx.Done():
		return message{}, ctx.Err()
	case ev, ok := <-r.proc.events:
		if !ok {
			return message{}, r.proc.err
		}

		return ev.message, ev.err
	}
}

// call keeps consuming notifications while waiting for its response; Codex
// may emit the whole turn before acknowledging turn/start.
func (r *run) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r.nextID++
	id := strconv.Itoa(r.nextID)
	stop := context.AfterFunc(ctx, func() { _ = r.proc.stdin.Close() })
	err := r.proc.send(map[string]any{"id": r.nextID, keyMethod: method, keyParams: params})

	stop()

	if err != nil {
		return nil, err
	}

	for {
		m, err := r.next(ctx)
		if err != nil {
			return nil, err
		}

		if m.Method == "" && string(m.ID) == id {
			if m.Error != nil {
				return nil, fmt.Errorf("codex %s: %w", method, m.Error)
			}

			return m.Result, nil
		}

		if err := r.handle(ctx, m); err != nil {
			return nil, err
		}
	}
}

type eventParams struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	RequestID json.RawMessage `json:"requestId"`
	Turn      struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
	Item struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Text    string `json:"text"`
		Phase   string `json:"phase"`
		Command string `json:"command"`
		Tool    string `json:"tool"`
		Server  string `json:"server"`
		Changes []struct {
			Path string `json:"path"`
			Diff string `json:"diff"`
		} `json:"changes"`
	} `json:"item"`
}

func (r *run) handle(ctx context.Context, m message) error {
	if m.Method == "" {
		return nil
	}

	var p eventParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return fmt.Errorf("codex %s: %w", m.Method, err)
	}

	if p.ThreadID != "" && r.threadID != "" && p.ThreadID != r.threadID {
		return nil
	}

	if p.TurnID != "" && r.turnID != "" && p.TurnID != r.turnID {
		return nil
	}

	if len(m.ID) != 0 {
		r.prompt(ctx, m)
		return nil
	}

	r.notification(m.Method, p)

	return nil
}

func (r *run) notification(method string, p eventParams) {
	switch method {
	case "turn/started":
		r.turnID = p.Turn.ID
	case "turn/completed":
		r.complete(p)
	case "serverRequest/resolved":
		if cancel := r.prompts[string(p.RequestID)]; cancel != nil {
			cancel()
			delete(r.prompts, string(p.RequestID))
		}
	case "item/started":
		r.progress(p)
	case "item/completed":
		r.agentMessage(p)
	}
}

func (r *run) agentMessage(p eventParams) {
	if p.Item.Type != "agentMessage" {
		return
	}

	switch p.Item.Phase {
	case "final_answer":
		r.final = p.Item.Text
	case "":
		r.fallback = p.Item.Text
	case "commentary":
		r.handler.Progress(agent.Event{Kind: agent.KindText, Text: p.Item.Text})
	}
}

func (r *run) complete(p eventParams) {
	r.finished = true

	switch p.Turn.Status {
	case "completed":
	case "interrupted":
		r.turnErr = agent.ErrCancelled
	default:
		text := p.Turn.Status
		if p.Turn.Error != nil {
			text = p.Turn.Error.Message
		}

		r.turnErr = fmt.Errorf("codex turn failed: %s", text)
	}
}

func (r *run) progress(p eventParams) {
	e := agent.Event{Kind: agent.KindTool, Tool: p.Item.Type}
	switch p.Item.Type {
	case "commandExecution":
		e.Detail = p.Item.Command
	case "fileChange":
		var details []string
		for _, change := range p.Item.Changes {
			details = append(details, change.Path+"\n"+change.Diff)
		}

		r.items[p.Item.ID] = strings.Join(details, "\n")
		e.Detail = r.items[p.Item.ID]
	case "mcpToolCall":
		e.Tool = p.Item.Server + "/" + p.Item.Tool
	case "webSearch", "dynamicToolCall":
	default:
		return
	}

	r.handler.Progress(e)
}

func (r *run) answer() (agent.Answer, error) {
	if r.turnErr != nil {
		return agent.Answer{}, r.turnErr
	}

	raw := r.final
	if raw == "" {
		raw = r.fallback
	}

	if strings.TrimSpace(raw) == "" {
		return agent.Answer{}, errors.New("codex completed without an answer")
	}

	a, err := agent.DecodeAnswer(raw, json.RawMessage(raw))
	if err != nil {
		return agent.Answer{}, err
	}

	if strings.TrimSpace(a.Summary) == "" || strings.TrimSpace(a.Full) == "" {
		return agent.Answer{}, errors.New("codex answer is missing summary or full")
	}

	return a, nil
}

func (r *run) interrupt(ctx context.Context) {
	if r.threadID == "" || r.turnID == "" || r.finished {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	// Closing stdin on timeout also unblocks a stalled writer.
	stop := context.AfterFunc(ctx, func() { r.proc.stop(); _ = r.proc.stdin.Close() })
	defer stop()

	_, err := r.call(ctx, "turn/interrupt", map[string]string{keyThreadID: r.threadID, keyTurnID: r.turnID})
	for err == nil && !r.finished {
		var m message

		m, err = r.next(ctx)
		if err == nil {
			err = r.handle(ctx, m)
		}
	}

	if err != nil {
		r.logger.DebugContext(ctx, "codex interrupt failed", keyError, err)
	}
}
