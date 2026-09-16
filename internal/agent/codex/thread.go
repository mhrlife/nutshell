package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mhrlife/nutshell/internal/agent"
)

const resumeThread = "thread/resume"

// A provisional conversation may not have a rollout yet: Codex creates one
// only when the first turn starts. Retain its ID in case that turn ran before
// the connection was lost, but allow recreation if Codex confirms it is absent.
type conversation struct {
	id          string
	provisional bool
}

func (a *Agent) openThread(ctx context.Context, req agent.Request, r *run) (string, error) {
	method, params, err := a.threadParams(req)
	if err != nil {
		return "", err
	}

	result, err := r.call(ctx, method, params)

	prior := a.sessions[req.Thread.Name()]
	if method == resumeThread && prior.provisional && missingRollout(err, prior.id) {
		delete(a.sessions, req.Thread.Name())

		method, params, err = a.threadParams(req)
		if err != nil {
			return "", err
		}

		result, err = r.call(ctx, method, params)
	}

	if err != nil {
		return "", err
	}

	var opened struct {
		Model  string `json:"model"`
		Cwd    string `json:"cwd"`
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &opened); err != nil {
		return "", fmt.Errorf("codex thread: %w", err)
	}

	if opened.Thread.ID == "" {
		return "", errors.New("codex returned no thread id")
	}

	if r.handler != nil && (opened.Model != "" || opened.Cwd != "") {
		r.handler.Progress(agent.Event{Kind: agent.KindMetadata, Model: opened.Model, Cwd: opened.Cwd})
	}

	a.sessions[req.Thread.Name()] = conversation{id: opened.Thread.ID, provisional: method != resumeThread}

	return opened.Thread.ID, nil
}

func missingRollout(err error, id string) bool {
	rpcErr, ok := errors.AsType[*rpcError](err)
	return ok && rpcErr.Code == -32600 && rpcErr.Message == "no rollout found for thread id "+id
}

func (a *Agent) threadParams(req agent.Request) (string, map[string]any, error) {
	params := map[string]any{"developerInstructions": agent.Instructions(req.Language, agent.Structured)}
	if own := a.sessions[req.Thread.Name()]; own.id != "" {
		params[keyThreadID] = own.id
		return resumeThread, params, nil
	}

	if req.Thread.Parent != "" {
		parent := a.sessions[req.Thread.Parent]
		if parent.id == "" {
			return "", nil, fmt.Errorf("codex parent thread %q has no conversation", req.Thread.Parent)
		}

		params[keyThreadID] = parent.id

		return "thread/fork", params, nil
	}

	return "thread/start", params, nil
}
