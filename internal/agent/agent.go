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
	// Selection is the passage of an earlier answer the question is about,
	// as the user selected it on screen, or "" when the question stands on
	// its own. Agents send Message rather than Text so the two arrive together.
	Selection string
	// Language is what the browser had selected when they asked it. A
	// language nutshell has no rules for arrives as lang.Lookup returns it,
	// and agents fall back to language-agnostic instructions.
	Language lang.Language
	// Thread is the conversation this question belongs to. The zero value is
	// the one nutshell starts in.
	Thread Thread
	// Notes are the conclusions of side threads the user finished since the
	// last question on this thread. Agents send Message, which carries them.
	Notes []Note
}

// Unasked is told about the turns an agent takes on its own. An agent that
// starts work in the background — Claude Code launches a task, answers
// without waiting for it, and picks the conversation up again by itself once
// it finishes — has something to say that belongs to no Ask call. nutshell
// gives such a turn a turn of its own, so its answer is shown, logged and
// read aloud like any other instead of being lost.
type Unasked interface {
	// Begin opens one such turn on thread. It returns the Handler the turn's
	// progress and prompts go to, and the function that ends the turn with
	// the answer it reached or the error that stopped it; an error means
	// there is no answer.
	Begin(thread Thread) (h Handler, end func(Answer, error))
}

// Agent is a conversational coding agent bound to one working directory.
// Implementations keep conversation history between Ask calls, one history
// per Request.Thread.
type Agent interface {
	// Name is a short human-readable label shown in the UI, e.g. "claude code".
	Name() string
	// Ask sends one user message and blocks until the final answer arrives.
	// h receives progress events and any prompt the agent needs answered
	// before it can carry on. A question for a thread the agent has not seen
	// before opens that thread as a copy of Request.Thread.Parent, so what
	// was said on the parent is known and what follows never reaches it.
	Ask(ctx context.Context, req Request, h Handler) (Answer, error)
	// Watch says where the agent reports the turns it takes without being
	// asked; see Unasked. It is called once, before the first Ask. An agent
	// that never takes such a turn may ignore it.
	Watch(Unasked)
	// Cancel aborts the turn in progress, if any. The next Ask resumes the conversation.
	Cancel()
	// Close releases the agent's resources.
	Close() error
}
