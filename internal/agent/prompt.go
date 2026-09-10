package agent

import (
	"context"
	"errors"
)

// ErrDismissed is returned by Handler.Prompt when the user closed a prompt
// without answering it. Agents turn it into a refusal.
var ErrDismissed = errors.New("agent: prompt dismissed")

// PromptKind says what the agent is waiting for.
type PromptKind string

const (
	// PromptPermission asks whether the agent may go ahead with something.
	PromptPermission PromptKind = "permission"
	// PromptChoice asks the user to answer a question the agent raised.
	PromptChoice PromptKind = "choice"
)

// Option IDs that every permission Prompt offers. Choice prompts use their
// own free-form IDs instead.
const (
	OptionAllow  = "allow"
	OptionAlways = "always"
	OptionDeny   = "deny"
)

// Prompt is something the agent needs from the user before the turn can go
// on. Everything is answered by picking options: a permission request is one
// Question offering allow and deny, and a question the agent asked is one
// Question per thing it wants to know.
type Prompt struct {
	ID   string     `json:"id"`
	Kind PromptKind `json:"kind"`
	// Title names what is being asked about, e.g. the tool. It may be empty
	// when the questions speak for themselves.
	Title string `json:"title,omitempty"`
	// Detail is the input the agent wants to act on: the command, the path.
	Detail    string     `json:"detail,omitempty"`
	Questions []Question `json:"questions"`
}

// Question is one decision inside a Prompt.
type Question struct {
	ID string `json:"id"`
	// Text is the question itself, left empty when Title already says it.
	Text string `json:"text,omitempty"`
	// Label is a short tag for the question, e.g. "indentation".
	Label string `json:"label,omitempty"`
	// Multi allows more than one Option to be picked.
	Multi bool `json:"multi,omitempty"`
	// FreeText allows the user to type their own answer instead of picking
	// an option. Such an answer arrives in the Reply where an option ID
	// would be, so anything not matching an Option ID is what they typed.
	FreeText bool     `json:"freeText,omitempty"`
	Options  []Option `json:"options"`
}

// Option is one answer offered for a Question.
type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Reply answers a Prompt with the IDs of the options picked for each
// Question, or, where the Question allows it, the answer the user typed.
type Reply struct {
	Choices map[string][]string `json:"choices"`
}

// First returns the one option picked for question id, or "" when it went
// unanswered.
func (r Reply) First(id string) string {
	if picked := r.Choices[id]; len(picked) > 0 {
		return picked[0]
	}

	return ""
}

// Handler is how an agent reaches the user while it works on a turn.
type Handler interface {
	// Progress reports one intermediate Event. It is called from a single
	// goroutine, in order.
	Progress(Event)
	// Prompt puts p to the user and blocks until they answer. Agents call it
	// from several goroutines at once, one per pending question. It returns
	// an error when the user dismissed the prompt or ctx ended first.
	Prompt(ctx context.Context, p Prompt) (Reply, error)
}

// ProgressFunc turns a plain progress callback into a Handler that dismisses
// every prompt, for callers with no way to reach the user.
type ProgressFunc func(Event)

// Progress implements Handler.
func (f ProgressFunc) Progress(ev Event) { f(ev) }

// Prompt implements Handler by dismissing the prompt.
func (ProgressFunc) Prompt(context.Context, Prompt) (Reply, error) {
	return Reply{}, ErrDismissed
}
