package codex

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mhrlife/nutshell/internal/agent"
)

const (
	commandApproval     = "item/commandExecution/requestApproval"
	fileApproval        = "item/fileChange/requestApproval"
	permissionsApproval = "item/permissions/requestApproval"
	userInput           = "item/tool/requestUserInput"
	decisionKey         = "decision"
	accept              = "accept"
	decline             = "decline"
)

type approvalParams struct {
	ItemID                string            `json:"itemId"`
	Command               string            `json:"command"`
	Cwd                   string            `json:"cwd"`
	Reason                string            `json:"reason"`
	GrantRoot             string            `json:"grantRoot"`
	Permissions           json.RawMessage   `json:"permissions"`
	AdditionalPermissions json.RawMessage   `json:"additionalPermissions"`
	AvailableDecisions    []json.RawMessage `json:"availableDecisions"`
	Questions             []inputQuestion   `json:"questions"`
}

type inputQuestion struct {
	ID       string `json:"id"`
	Header   string `json:"header"`
	Question string `json:"question"`
	IsOther  bool   `json:"isOther"`
	IsSecret bool   `json:"isSecret"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

func (r *run) prompt(ctx context.Context, m message) {
	var params approvalParams
	if err := json.Unmarshal(m.Params, &params); err != nil {
		_ = r.proc.send(map[string]any{"id": m.ID, keyError: rpcError{Code: -32602, Message: "invalid request params"}})
		return
	}

	p, ok := buildPrompt(m, params, r.items[params.ItemID])
	if !ok {
		_ = r.proc.send(map[string]any{"id": m.ID, keyError: rpcError{Code: -32601, Message: "nutshell does not support " + m.Method}})
		return
	}

	if len(r.prompts) >= 64 {
		_ = r.proc.send(map[string]any{"id": m.ID, keyError: rpcError{Code: -32603, Message: "too many pending prompts"}})
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	r.prompts[string(m.ID)] = cancel
	r.workers.Go(func() {
		defer cancel()

		reply, err := r.handler.Prompt(ctx, p)
		if err != nil {
			reply = agent.Reply{}
		}

		if err := r.proc.send(map[string]any{"id": m.ID, "result": promptResult(m.Method, params, reply)}); err != nil {
			r.logger.DebugContext(ctx, "codex prompt reply failed", keyError, err)
		}
	})
}

func buildPrompt(m message, params approvalParams, changes string) (agent.Prompt, bool) {
	p := agent.Prompt{ID: "codex:" + string(m.ID), Kind: agent.PromptPermission, Title: m.Method}
	switch m.Method {
	case userInput:
		p.Kind = agent.PromptChoice
		p.Title = ""

		for _, q := range params.Questions {
			// The current UI logs and displays answers; it is not a secret input.
			if q.IsSecret {
				return agent.Prompt{}, false
			}

			question := agent.Question{ID: q.ID, Text: q.Question, Label: q.Header, FreeText: q.IsOther || len(q.Options) == 0}
			for _, o := range q.Options {
				question.Options = append(question.Options, agent.Option{ID: o.Label, Label: o.Label, Detail: o.Description})
			}

			p.Questions = append(p.Questions, question)
		}

		return p, len(p.Questions) > 0
	case commandApproval:
		p.Title = "Run command"
		p.Detail = joinDetails(params.Command, params.Cwd, params.Reason, string(params.AdditionalPermissions))
	case fileApproval:
		p.Title = "Change files"
		p.Detail = joinDetails(changes, params.Reason, params.GrantRoot)
	case permissionsApproval:
		p.Title = "Grant permissions for this turn"
		p.Detail = joinDetails(params.Reason, string(params.Permissions))
	default:
		return p, false
	}

	options := []agent.Option{}
	if m.Method != commandApproval || allowsDecision(params, accept) {
		options = append(options, agent.Option{ID: agent.OptionAllow, Label: "Allow once"})
	}

	options = append(options, agent.Option{ID: agent.OptionDeny, Label: "Deny"})
	p.Questions = []agent.Question{{ID: decisionKey, Options: options}}

	return p, true
}

func allowsDecision(p approvalParams, decision string) bool {
	if len(p.AvailableDecisions) == 0 {
		return true
	}

	for _, raw := range p.AvailableDecisions {
		var value string
		if json.Unmarshal(raw, &value) == nil && value == decision {
			return true
		}
	}

	return false
}

func promptResult(method string, p approvalParams, reply agent.Reply) any {
	if method == userInput {
		return inputResult(p, reply)
	}

	allowed := reply.First(decisionKey) == agent.OptionAllow

	if method == permissionsApproval {
		permissions := json.RawMessage(`{}`)
		if allowed && len(p.Permissions) > 0 && string(p.Permissions) != "null" {
			permissions = p.Permissions
		}

		return map[string]any{"permissions": permissions, "scope": keyTurn}
	}

	decision := decline
	if allowed && (method != commandApproval || allowsDecision(p, accept)) {
		decision = accept
	}

	if method == commandApproval && decision == decline && !allowsDecision(p, decline) {
		decision = "cancel"
	}

	return map[string]string{decisionKey: decision}
}

func inputResult(p approvalParams, reply agent.Reply) any {
	answers := map[string]any{}

	for _, q := range p.Questions {
		picked := reply.Choices[q.ID]
		if picked == nil {
			picked = []string{}
		}

		answers[q.ID] = map[string]any{"answers": picked}
	}

	return map[string]any{"answers": answers}
}

func joinDetails(parts ...string) string {
	var present []string

	for _, p := range parts {
		if p != "" && p != "null" {
			present = append(present, p)
		}
	}

	return strings.Join(present, "\n\n")
}
