package claudecode

// Not every turn in a process's stream was asked for. When a background task
// claude started earlier finishes, claude picks the conversation up again by
// itself: a task_notification, a fresh turn, and a second result — with no
// question in front of it and nobody waiting for it.
//
// So the stream is read by one goroutine for the whole life of the process
// rather than by whoever happens to be waiting on an answer. It reads without
// pause — stdout is a pipe, and a reader that stops between questions
// eventually blocks the process it is meant to be driving — and routes every
// turn it finds: to the Ask waiting for one, or, when no Ask is waiting, to
// the agent.Unasked watcher, which gives the turn a place of its own.

import (
	"context"

	"github.com/mhrlife/nutshell/internal/agent"
)

// subtypeInit is the system event claude writes at the head of every turn,
// including one it starts by itself. Nothing at all comes out of the process
// until a turn begins, so an init with no turn claimed is the one reliable
// sign that claude has started one of its own.
const subtypeInit = "init"

// turn is one turn being read off a process's stream.
type turn struct {
	handler agent.Handler
	// end reports the outcome of a turn nobody asked for. It is nil for a
	// turn an Ask is waiting on, whose outcome goes to done instead.
	end  func(agent.Answer, error)
	done chan outcome
}

// outcome is how a turn ended: with the answer claude reached, or with the
// error that stopped it.
type outcome struct {
	answer agent.Answer
	err    error
}

// newTurn is a turn an Ask is waiting for. done is buffered so that reporting
// the outcome never waits for the asker, who may already have given up on it.
func newTurn(h agent.Handler) *turn {
	return &turn{handler: h, done: make(chan outcome, 1)}
}

// finish hands the turn's outcome to whoever the turn belongs to.
func (t *turn) finish(answer agent.Answer, err error) {
	if t.end != nil {
		t.end(answer, err)

		return
	}

	t.done <- outcome{answer: answer, err: err}
}

// discard is the progress of a turn there is nobody to tell about.
func discard(agent.Event) {}

// claim makes t the turn being read, waiting out any turn already under way.
func (p *process) claim(ctx context.Context, t *turn) error {
	return p.settle(ctx, t)
}

// idle waits until no turn is being read, without taking the stream over.
func (p *process) idle(ctx context.Context) error {
	return p.settle(ctx, nil)
}

// settle waits for the stream to carry no turn and then, when t is not nil,
// makes it the turn being read.
func (p *process) settle(ctx context.Context, t *turn) error {
	for {
		p.turnMu.Lock()

		if p.turn == nil {
			if t != nil {
				p.turn = t
			}

			p.turnMu.Unlock()

			return nil
		}

		freed := p.freed

		p.turnMu.Unlock()

		select {
		case <-freed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// open returns the turn the stream is carrying, starting one with start when
// it is carrying none. Only the reader calls it, so it never waits: a turn
// nobody asked for begins the moment its first event arrives.
func (p *process) open(start func() *turn) *turn {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()

	if p.turn == nil {
		p.turn = start()
	}

	return p.turn
}

// release ends the turn being read and wakes whoever is waiting for the
// stream. It returns that turn, or nil when there was none.
func (p *process) release() *turn {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()

	t := p.turn
	p.turn = nil

	close(p.freed)
	p.freed = make(chan struct{})

	return t
}

// pump reads proc's stream until the process exits. ctx ends with the
// process, so a question still on the user's screen when it dies is withdrawn
// rather than left waiting for an answer nobody can give.
func (a *Agent) pump(ctx context.Context, proc *process) {
	for ev := range proc.events {
		a.route(ctx, proc, ev)
	}

	a.closeStream(proc)
}

// route handles one stream event: it answers what claude asks of its host,
// and reports everything else to the turn it belongs to.
func (a *Agent) route(ctx context.Context, proc *process, ev streamEvent) {
	if ev.SessionID != "" {
		a.setSessionID(ev.SessionID)
	}

	switch ev.Type {
	case typeControlCancel:
		a.withdrawPrompt(ev.RequestID)

		return
	case typeControl:
		// A permission request is part of a turn; one arriving between turns
		// means claude is working on its own again.
		t := a.begin(proc)

		go a.serveControl(ctx, proc, ev, t.handler)

		return
	case typeSystem:
		if ev.Subtype == subtypeInit {
			// The turn starts here, ahead of its first word, so that a
			// question asked in the meantime waits for it instead of being
			// answered with its result.
			a.begin(proc)
		}

		return
	case typeAssistant:
		a.report(proc, ev)

		return
	case typeResult:
		a.complete(proc, ev)

		return
	}
}

// report passes one assistant message to the turn it belongs to, starting a
// turn claude took on its own when there is none.
func (a *Agent) report(proc *process, ev streamEvent) {
	_, _, _ = handleEvent(ev, a.begin(proc).handler.Progress)
}

// complete ends the turn the result belongs to. A result with no turn behind
// it — claude finishing something it never said a word about — is still
// charged for, because what it reports is the cost of the whole process so
// far and the next turn's share is measured against it.
func (a *Agent) complete(proc *process, ev streamEvent) {
	answer, _, err := handleEvent(ev, discard)
	a.chargeTurn(&answer)

	a.withdrawAllPrompts()

	if t := proc.release(); t != nil {
		t.finish(answer, err)
	}
}

// begin returns the turn the stream is carrying, opening one nobody asked for
// when it is carrying none.
func (a *Agent) begin(proc *process) *turn {
	return proc.open(a.unaskedTurn)
}

// unaskedTurn opens a turn with the watcher, on the thread the process is
// carrying on. With no watcher installed there is nobody to tell: the turn is
// read and thrown away, and still holds the stream, so the next question
// waits for it rather than being answered with its result.
func (a *Agent) unaskedTurn() *turn {
	a.mu.Lock()
	watcher, thread := a.unasked, a.thread
	a.mu.Unlock()

	if watcher == nil {
		return &turn{handler: agent.ProgressFunc(discard), end: func(agent.Answer, error) {}}
	}

	handler, end := watcher.Begin(thread)

	return &turn{handler: handler, end: end}
}

// closeStream ends the turn the process was in the middle of when it died, so
// that nobody waits for an answer that is never coming.
func (a *Agent) closeStream(proc *process) {
	err := a.exitError(proc)

	if t := proc.release(); t != nil {
		t.finish(agent.Answer{}, err)
	}
}
