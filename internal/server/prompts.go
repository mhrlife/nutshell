package server

// A server-sent-event stream only runs one way, so a question from the agent
// and the user's answer travel separately: the question goes out on the
// turn's event stream, and the browser posts the answer back to /api/answer.
// The desk is what pairs the two up.

import (
	"context"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
)

// promptDesk holds the questions waiting for an answer, keyed by prompt ID.
type promptDesk struct {
	mu      sync.Mutex
	waiting map[string]chan agent.Reply
}

func newPromptDesk() *promptDesk {
	return &promptDesk{waiting: map[string]chan agent.Reply{}}
}

// wait registers a prompt and returns the channel its answer will arrive on.
func (d *promptDesk) wait(id string) <-chan agent.Reply {
	replies := make(chan agent.Reply, 1)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.waiting[id] = replies

	return replies
}

// forget drops a prompt, whether it was answered or given up on.
func (d *promptDesk) forget(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.waiting, id)
}

// answer delivers a reply. It reports false when no such prompt is waiting,
// which is what the browser sees after a prompt has already been withdrawn.
func (d *promptDesk) answer(id string, reply agent.Reply) bool {
	d.mu.Lock()
	replies := d.waiting[id]
	d.mu.Unlock()

	if replies == nil {
		return false
	}

	select {
	case replies <- reply:
		return true
	default:
		return false // already answered
	}
}

// turnHandler is the agent.Handler for one turn: it writes the agent's
// progress and questions to the session log, which is what the browser reads.
type turnHandler struct {
	turn int
	log  *session
	desk *promptDesk
}

// Progress implements agent.Handler.
func (h *turnHandler) Progress(ev agent.Event) {
	h.log.add(h.turn, string(ev.Kind), ev)
}

// Prompt implements agent.Handler: it puts the prompt on screen and blocks
// until the user answers it or the agent withdraws it. Only the agent can
// withdraw it, so a prompt outlives the page that first showed it.
func (h *turnHandler) Prompt(ctx context.Context, p agent.Prompt) (agent.Reply, error) {
	replies := h.desk.wait(p.ID)
	defer h.desk.forget(p.ID)

	h.log.add(h.turn, kindPrompt, p)

	select {
	case reply := <-replies:
		h.log.add(h.turn, kindPromptDone, map[string]string{"id": p.ID})

		return reply, nil
	case <-ctx.Done():
		h.log.add(h.turn, kindPromptDone, map[string]string{"id": p.ID})

		return agent.Reply{}, ctx.Err()
	}
}
