package server

// Running turns: asking a question, opening a side thread for one, and
// finishing a side thread — which is itself a turn, the one that asks the
// thread what it settled before the answer is carried up.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

// handleAsk starts one turn and returns as soon as it is under way. The turn
// itself belongs to the session, so the browser can reload, go away, or come
// back while the agent works; everything it produces is written to the log and
// read from /api/stream.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text      string `json:"text"`
		Selection string `json:"selection"` // passage of an earlier answer the question is about
		Lang      string `json:"lang"`      // language the question was asked in
		Thread    string `json:"thread"`    // the conversation it belongs to; empty is the main one
		Fork      bool   `json:"fork"`      // open a side thread for it instead of asking here
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		s.writeError(w, r, http.StatusBadRequest, errors.New("empty question"))

		return
	}

	if !s.busy.CompareAndSwap(false, true) {
		s.writeError(w, r, http.StatusConflict, errors.New("a question is already in progress"))

		return
	}

	thread, err := s.threadFor(in.Thread, in.Fork, in.Selection, in.Text)
	if err != nil {
		s.busy.Store(false)
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	req, err := s.request(thread, in.Text, in.Selection, in.Lang)
	if err != nil {
		s.busy.Store(false)
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	// Deliberately detached from the request: this work outlives it.
	ctx := context.WithoutCancel(r.Context())
	turn := s.session.startTurn(question{thread: thread, text: in.Text, selection: agent.Excerpt(in.Selection)})

	s.threads.record(thread, in.Text)

	go func() { _, _ = s.runTurn(ctx, turn, req) }()

	writeJSON(w, http.StatusAccepted, map[string]any{"turn": turn, "thread": thread})
}

// threadFor is the thread a question is to be asked on: the one the browser
// is showing, or a side thread opened from it. Opening one is recorded in the
// thread above, which is where the user was when they opened it.
func (s *Server) threadFor(thread string, fork bool, selection, text string) (string, error) {
	if thread == "" {
		thread = agent.RootThread
	}

	if !fork {
		return thread, nil
	}

	id, err := s.threads.fork(thread, agent.Excerpt(selection), text)
	if err != nil {
		return "", err
	}

	// The title says what the side thread is about — the passage, when it was
	// opened from one. The question goes along with it because that is what
	// the trail shows: your own words, not a passage you only pointed at.
	s.session.mark(thread, kindThread, map[string]any{
		"id":       id,
		"title":    s.threads.title(id),
		"question": trimTitle(text),
	})

	return id, nil
}

// request is one question ready for the agent, with whatever finished side
// threads left for this one to hear about.
func (s *Server) request(thread, text, selection, code string) (agent.Request, error) {
	th, notes, err := s.threads.request(thread)
	if err != nil {
		return agent.Request{}, err
	}

	return agent.Request{
		Text:      text,
		Selection: selection,
		Language:  lang.Lookup(code),
		Thread:    th,
		Notes:     notes,
	}, nil
}

// handleClose finishes a side thread. Without inject it is simply closed;
// with it, the thread is asked what it settled first, and that answer becomes
// the note the thread above hears on its next question.
func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Thread string `json:"thread"`
		Inject bool   `json:"inject"` // carry the conclusion up to the thread above
		Lang   string `json:"lang"`   // language the conclusion is to be written in
	}

	if err := decode(w, r, maxTextRequest, &in); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	if !in.Inject {
		if err := s.closeThread(in.Thread, agent.Note{}); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)

			return
		}

		w.WriteHeader(http.StatusNoContent)

		return
	}

	if !s.busy.CompareAndSwap(false, true) {
		s.writeError(w, r, http.StatusConflict, errors.New("a question is already in progress"))

		return
	}

	req, err := s.request(in.Thread, agent.Conclusion(), "", in.Lang)
	if err != nil {
		s.busy.Store(false)
		s.writeError(w, r, http.StatusBadRequest, err)

		return
	}

	ctx := context.WithoutCancel(r.Context())
	turn := s.session.startTurn(question{thread: in.Thread, closing: true})

	go s.concludeThread(ctx, turn, req)

	writeJSON(w, http.StatusAccepted, map[string]any{"turn": turn, "thread": in.Thread})
}

// concludeThread runs the closing turn and, if it produced an answer, closes
// the thread with it. A turn that failed or was cancelled leaves the thread
// open, so the user can go on with it or close it without a conclusion.
func (s *Server) concludeThread(ctx context.Context, turn int, req agent.Request) {
	answer, ok := s.runTurn(ctx, turn, req)
	if !ok {
		return
	}

	if err := s.closeThread(req.Thread.ID, s.threads.conclude(req.Thread.ID, answer.Summary)); err != nil {
		s.logger.ErrorContext(ctx, "closing a side thread", "thread", req.Thread.ID, "error", err)
	}
}

// closeThread finishes a thread and tells the thread above about it. An empty
// note is a thread closed without carrying anything up. Side threads still
// open below this one go with it, each recorded in its own parent.
func (s *Server) closeThread(id string, note agent.Note) error {
	closed, err := s.threads.finish(id)
	if err != nil {
		return err
	}

	for i, th := range closed {
		done := map[string]any{"id": th.id, "title": th.title, "injected": false}

		if i == 0 && strings.TrimSpace(note.Conclusion) != "" {
			if err := s.threads.note(th.parent, note); err != nil {
				return err
			}

			done["injected"] = true
			done["conclusion"] = note.Conclusion
		}

		s.session.mark(th.parent, kindThreadDone, done)
	}

	return nil
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.agent.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

// runTurn works through one question and records how it ended. Whatever
// happens, the turn ends with an entry on the stream: a page left waiting on
// silence is the one outcome the UI cannot explain. It reports the answer,
// and whether there was one at all.
func (s *Server) runTurn(ctx context.Context, turn int, req agent.Request) (agent.Answer, bool) {
	started := time.Now()
	thread := req.Thread.Name()
	logger := s.logger.With("turn", turn, "thread", thread)

	defer s.busy.Store(false)
	defer s.recoverTurn(ctx, turn, req)

	logger.DebugContext(ctx, "turn started", "lang", req.Language.Code)

	answer, err := s.agent.Ask(ctx, req, &turnHandler{turn: turn, thread: thread, log: s.session, desk: s.prompts})

	logger.DebugContext(ctx, "turn finished", "ms", time.Since(started).Milliseconds(), "error", err)

	switch {
	case errors.Is(err, agent.ErrCancelled), errors.Is(err, context.Canceled):
		s.restoreNotes(req)
		s.session.add(thread, turn, kindError, cancelledEntry())
	case err != nil:
		logger.ErrorContext(ctx, "ask failed", "error", err)
		s.restoreNotes(req)
		s.session.add(thread, turn, kindError, map[string]string{keyMessage: err.Error()})
	default:
		s.session.add(thread, turn, kindResult, answer)

		return answer, true
	}

	return agent.Answer{}, false
}

// restoreNotes puts the conclusions this turn was carrying back where they
// were waiting. The agent never heard them, so the next question must.
func (s *Server) restoreNotes(req agent.Request) {
	for _, note := range req.Notes {
		if err := s.threads.note(req.Thread.Name(), note); err != nil {
			s.logger.Error("keeping a side thread's conclusion", "thread", req.Thread.Name(), "error", err)
		}
	}
}

// recoverTurn turns a panic in the agent into a visible failure instead of a
// dead process and a page that waits for ever.
func (s *Server) recoverTurn(ctx context.Context, turn int, req agent.Request) {
	p := recover()
	if p == nil {
		return
	}

	s.logger.ErrorContext(ctx, "turn panicked", "turn", turn, "panic", p, "stack", string(debug.Stack()))
	s.restoreNotes(req)
	s.session.add(req.Thread.Name(), turn, kindError, map[string]string{keyMessage: fmt.Sprintf("the agent crashed: %v", p)})
}
