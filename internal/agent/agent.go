// Package agent defines the contract between nutshell and the coding agent
// that answers questions. Claude Code is the first implementation; others
// (Codex, ...) plug in behind the same interface.
package agent

import (
	"context"
	"errors"

	"github.com/mhrlife/nutshell/internal/lang"
)

// ErrCancelled is returned by Ask when the turn was aborted through Cancel.
var ErrCancelled = errors.New("agent: turn cancelled")

// Kind classifies a progress Event.
type Kind string

const (
	// KindTool reports that the agent invoked a tool.
	KindTool Kind = "tool"
	// KindText reports intermediate text the agent wrote before its final answer.
	KindText Kind = "text"
)

// Event is a progress update emitted while the agent works on a turn.
type Event struct {
	Kind   Kind   `json:"type"`
	Tool   string `json:"name,omitempty"`   // tool name, for KindTool
	Detail string `json:"detail,omitempty"` // one-line description of the tool input
	Text   string `json:"text,omitempty"`   // intermediate text, for KindText
}

// Answer is the parsed final reply of one turn.
type Answer struct {
	// Summary answers only the user's question, in a few spoken sentences.
	Summary string `json:"summary"`
	// Full is the complete answer in Markdown.
	Full string `json:"full"`
	// Raw is the agent's reply before parsing.
	Raw string `json:"raw"`
	// CostUSD is what this turn cost at the agent, when the agent reports it.
	CostUSD float64 `json:"cost_usd"`
	// CostKnown is false for agents that do not report costs.
	CostKnown bool `json:"cost_known"`
}

// Request is one user message together with the language it was spoken in.
// The language travels with every turn instead of being fixed at startup,
// because it is chosen in the browser and can change between two questions.
type Request struct {
	// Text is what the user asked.
	Text string
	// Language is what the browser had selected when they asked it. A
	// language nutshell has no rules for arrives as lang.Lookup returns it,
	// and agents fall back to language-agnostic instructions.
	Language lang.Language
}

// Agent is a conversational coding agent bound to one working directory.
// Implementations keep conversation history between Ask calls.
type Agent interface {
	// Name is a short human-readable label shown in the UI, e.g. "claude code".
	Name() string
	// Ask sends one user message and blocks until the final answer arrives.
	// h receives progress events and any prompt the agent needs answered
	// before it can carry on.
	Ask(ctx context.Context, req Request, h Handler) (Answer, error)
	// Cancel aborts the turn in progress, if any. The next Ask resumes the conversation.
	Cancel()
	// Close releases the agent's resources.
	Close() error
}
