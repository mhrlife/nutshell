package claudecode

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
)

// The stream event types nutshell acts on. Everything else claude writes —
// the user messages it echoes back, its rate-limit notices, the rest of the
// system events — is read and passed over.
const (
	typeSystem        = "system"
	typeAssistant     = "assistant"
	typeResult        = "result"
	typeControl       = "control_request"
	typeControlCancel = "control_cancel_request"
)

// structuredOutputTool is the tool claude hands its final reply to when it is
// held to --json-schema. The call is the answer itself, not a step towards it.
const structuredOutputTool = "StructuredOutput"

// streamEvent is one line of `claude --output-format stream-json`.
type streamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Message   json.RawMessage `json:"message"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	// StructuredOutput is the final reply as the object --json-schema
	// describes; Result then carries the same object as text.
	StructuredOutput json.RawMessage `json:"structured_output"`
	// RequestID and Request carry a control request: claude asking its host
	// for permission, or for an answer to a question.
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	// TotalCostUSD is cumulative for the life of the process.
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// contentBlock is one block of an assistant message.
type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input"`
}

// resultAnswer reads the answer out of a successful result. The schema is
// checked, not guaranteed: claude hands a rejected reply back to the model,
// but a model that keeps failing it ends the turn in plain text, and the
// result still reports success. Such a result carries no structured output and
// is split as a tagged reply, which with no tags in it is the whole text as
// both parts.
func resultAnswer(ev streamEvent) (agent.Answer, error) {
	if len(ev.StructuredOutput) == 0 || string(ev.StructuredOutput) == "null" {
		return agent.ParseAnswer(ev.Result), nil
	}

	answer, err := agent.DecodeAnswer(ev.Result, ev.StructuredOutput)
	if err != nil {
		return agent.Answer{}, fmt.Errorf("claude code: %w", err)
	}

	return answer, nil
}

func parseBlocks(raw json.RawMessage) []contentBlock {
	var m struct {
		Content json.RawMessage `json:"content"`
	}

	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}

	var blocks []contentBlock
	// A tool_result message carries a string here; ignoring the error is intended.
	_ = json.Unmarshal(m.Content, &blocks)

	return blocks
}

const maxDetailRunes = 140

// describeInput turns a tool call's input into one short line for the UI.
func describeInput(input json.RawMessage) string {
	var m map[string]any

	_ = json.Unmarshal(input, &m)

	s := firstString(m, "command", "file_path", "pattern", "url", "query", "description", "prompt", "path")
	if s == "" {
		b, _ := json.Marshal(m)
		s = string(b)
	}

	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > maxDetailRunes {
		s = string(r[:maxDetailRunes]) + "…"
	}

	return s
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}

	return ""
}

// tailBuffer keeps the last max bytes written to it. It captures the end of
// the process's stderr for error messages.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}

	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return string(t.buf)
}
