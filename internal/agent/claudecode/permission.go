package claudecode

// Claude Code asks its host for two things over the same wire: permission to
// run a tool, and the answers to a question it raised with AskUserQuestion.
// Both arrive as a "can_use_tool" control request, and both are answered by
// allowing the tool call — a question carries the user's answers back inside
// the tool input. This file turns those requests into agent.Prompts and the
// user's replies back into control responses.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mhrlife/nutshell/internal/agent"
)

// askQuestionTool is the Claude Code tool that puts a question to the user.
const askQuestionTool = "AskUserQuestion"

// Wire values of the control protocol.
const (
	subtypeCanUseTool = "can_use_tool"
	behaviorAllow     = "allow"
	behaviorDeny      = "deny"
)

// controlResponse is what the host writes back for one control request.
type controlResponse struct {
	Type     string       `json:"type"`
	Response responseBody `json:"response"`
}

type responseBody struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  any    `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

// decision is the host's verdict on one tool call. A question is answered by
// allowing the call with the answers written into UpdatedInput.
type decision struct {
	Behavior           string          `json:"behavior"`
	Message            string          `json:"message,omitempty"`
	UpdatedInput       any             `json:"updatedInput,omitempty"`
	UpdatedPermissions json.RawMessage `json:"updatedPermissions,omitempty"`
}

// permissionDecision is the ID of the single question a permission prompt asks.
const permissionDecision = "decision"

// controlRequest is the payload of a control_request stream event. Only the
// can_use_tool fields are read; anything else is refused.
type controlRequest struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	DisplayName string          `json:"display_name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Suggestions json.RawMessage `json:"permission_suggestions"`
}

// serveControl answers one control request. It runs in its own goroutine
// because the user may take as long as they like, and Claude Code can have
// several requests outstanding at once.
func (a *Agent) serveControl(ctx context.Context, proc *process, ev streamEvent, h agent.Handler) {
	var req controlRequest
	if err := json.Unmarshal(ev.Request, &req); err != nil || req.Subtype != subtypeCanUseTool {
		_ = proc.send(controlError(ev.RequestID, "nutshell cannot answer "+req.Subtype+" requests"))

		return
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.holdPrompt(ev.RequestID, cancel)
	defer a.releasePrompt(ev.RequestID)

	reply, err := h.Prompt(ctx, buildPrompt(ev.RequestID, req))
	if err != nil {
		// A withdrawn request ignores whatever arrives late, so answering
		// unconditionally is safe and keeps the tool call from hanging.
		_ = proc.send(controlSuccess(ev.RequestID, denial("The user did not answer.")))

		return
	}

	_ = proc.send(controlSuccess(ev.RequestID, decide(req, reply)))
}

// holdPrompt remembers how to withdraw a prompt Claude Code later cancels.
func (a *Agent) holdPrompt(id string, cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.prompts == nil {
		a.prompts = map[string]context.CancelFunc{}
	}

	a.prompts[id] = cancel
}

func (a *Agent) releasePrompt(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.prompts, id)
}

// withdrawPrompt drops one pending prompt, on a control_cancel_request.
func (a *Agent) withdrawPrompt(id string) {
	a.mu.Lock()
	cancel := a.prompts[id]
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// withdrawAllPrompts drops every pending prompt, at the end of a turn.
func (a *Agent) withdrawAllPrompts() {
	a.mu.Lock()
	pending := a.prompts
	a.prompts = nil
	a.mu.Unlock()

	for _, cancel := range pending {
		cancel()
	}
}

// buildPrompt describes one control request in the terms the UI understands.
func buildPrompt(id string, req controlRequest) agent.Prompt {
	if req.ToolName == askQuestionTool {
		if questions := parseQuestions(req.Input); len(questions) > 0 {
			return agent.Prompt{ID: id, Kind: agent.PromptChoice, Questions: questions}
		}
	}

	return agent.Prompt{
		ID:        id,
		Kind:      agent.PromptPermission,
		Title:     firstNonEmpty(req.Title, req.DisplayName, req.ToolName),
		Detail:    firstNonEmpty(describeInput(req.Input), req.Description),
		Questions: []agent.Question{{ID: permissionDecision, Options: permissionOptions(req)}},
	}
}

// permissionOptions offers "always" only when Claude Code suggested a rule
// that would make the question go away.
func permissionOptions(req controlRequest) []agent.Option {
	options := []agent.Option{{ID: agent.OptionAllow, Label: "Allow once"}}

	if hasSuggestions(req.Suggestions) {
		options = append(options, agent.Option{ID: agent.OptionAlways, Label: "Always allow"})
	}

	return append(options, agent.Option{ID: agent.OptionDeny, Label: "Deny"})
}

func hasSuggestions(raw json.RawMessage) bool {
	var suggestions []json.RawMessage
	if err := json.Unmarshal(raw, &suggestions); err != nil {
		return false
	}

	return len(suggestions) > 0
}

// askQuestion is one entry of the AskUserQuestion tool input.
type askQuestion struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Multi    bool   `json:"multiSelect"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

func parseAskInput(raw json.RawMessage) []askQuestion {
	var in struct {
		Questions []askQuestion `json:"questions"`
	}

	if err := json.Unmarshal(raw, &in); err != nil {
		return nil
	}

	return in.Questions
}

// parseQuestions turns an AskUserQuestion input into prompt questions. An
// option's ID is its own label, which is what the answer must be written as.
func parseQuestions(raw json.RawMessage) []agent.Question {
	asked := parseAskInput(raw)

	questions := make([]agent.Question, 0, len(asked))

	for i, q := range asked {
		if len(q.Options) == 0 {
			continue
		}

		options := make([]agent.Option, 0, len(q.Options))
		for _, o := range q.Options {
			options = append(options, agent.Option{ID: o.Label, Label: o.Label, Detail: o.Description})
		}

		questions = append(questions, agent.Question{
			ID:    questionID(i),
			Text:  q.Question,
			Label: q.Header,
			Multi: q.Multi,
			// AskUserQuestion always lets the user write their own answer
			// instead of taking one of the options offered.
			FreeText: true,
			Options:  options,
		})
	}

	return questions
}

func questionID(i int) string { return fmt.Sprintf("q%d", i) }

// decide turns the user's reply into the body of a control response.
func decide(req controlRequest, reply agent.Reply) decision {
	if req.ToolName == askQuestionTool {
		if asked := parseAskInput(req.Input); len(asked) > 0 {
			return answerQuestions(req.Input, asked, reply)
		}
	}

	switch reply.First(permissionDecision) {
	case agent.OptionAllow:
		return approval(req.Input, nil)
	case agent.OptionAlways:
		return approval(req.Input, req.Suggestions)
	default:
		return denial("The user denied this.")
	}
}

// answerQuestions allows the AskUserQuestion call with the user's answers
// written into its input, which is where the tool reads them from.
func answerQuestions(raw json.RawMessage, asked []askQuestion, reply agent.Reply) decision {
	answers := map[string]string{}

	for i, q := range asked {
		if picked := reply.Choices[questionID(i)]; len(picked) > 0 {
			answers[q.Question] = strings.Join(picked, ", ")
		}
	}

	if len(answers) == 0 {
		return denial("The user did not answer the question.")
	}

	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil || input == nil {
		input = map[string]any{}
	}

	input["answers"] = answers

	return decision{Behavior: behaviorAllow, UpdatedInput: input}
}

func approval(input, suggestions json.RawMessage) decision {
	d := decision{Behavior: behaviorAllow, UpdatedInput: input}
	if hasSuggestions(suggestions) {
		d.UpdatedPermissions = suggestions
	}

	return d
}

func denial(message string) decision {
	return decision{Behavior: behaviorDeny, Message: message}
}

func controlSuccess(id string, resp any) controlResponse {
	return controlResponse{
		Type:     "control_response",
		Response: responseBody{Subtype: "success", RequestID: id, Response: resp},
	}
}

func controlError(id, message string) controlResponse {
	return controlResponse{
		Type:     "control_response",
		Response: responseBody{Subtype: "error", RequestID: id, Error: message},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
