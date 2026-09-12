package server

// A question can be asked without disturbing the conversation it came out of:
// it opens a side thread, which starts as a copy of that conversation and
// goes its own way. The tree of threads lives here — who came from whom, what
// each one is about, which ones are finished, and what a finished one left
// for the thread above it to hear about on its next question.

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
)

// threadTitle caps the subject kept for a side thread.
const threadTitle = 120

// errNoThread is what an id nobody opened comes back as.
var errNoThread = errors.New("no such thread")

// errThreadDone is what a question for a finished thread comes back as.
var errThreadDone = errors.New("that side thread is finished")

// thread is one conversation in the tree.
type thread struct {
	id     string
	parent string   // "" for the root thread
	title  string   // what this side thread is about, shown in the trail
	about  string   // the passage it was opened from, when it was opened from one
	asked  []string // the questions the user put to it, in the order they asked them
	closed bool
	notes  []agent.Note // conclusions waiting for this thread's next question
}

// threads is the tree, and the only place a thread's state is kept.
type threads struct {
	mu   sync.Mutex
	byID map[string]*thread
	next int
}

func newThreads() *threads {
	root := &thread{id: agent.RootThread}

	return &threads{byID: map[string]*thread{root.id: root}}
}

// fork opens a side thread of parent and returns its id. about is the passage
// it was opened from, if any, and question is what is about to be asked in
// it; between them they say what the thread is about. The conclusions parent
// has not passed on yet are copied into it: the new thread starts as a copy of
// parent's conversation, which has not heard them either.
func (t *threads) fork(parent, about, question string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	above, err := t.openLocked(parent)
	if err != nil {
		return "", err
	}

	about = trimTitle(about)

	title := about
	if title == "" {
		title = trimTitle(question)
	}

	t.next++
	child := &thread{
		id:     fmt.Sprintf("t%d", t.next),
		parent: above.id,
		title:  title,
		about:  about,
		notes:  append([]agent.Note(nil), above.notes...),
	}
	t.byID[child.id] = child

	return child.id, nil
}

// record keeps a question the user asked, so that when the thread is finished
// the one above is told what was being asked, not only what came of it.
func (t *threads) record(id, question string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if th := t.byID[id]; th != nil {
		th.asked = append(th.asked, question)
	}
}

// conclude is the note a thread leaves behind, once conclusion is known.
func (t *threads) conclude(id, conclusion string) agent.Note {
	t.mu.Lock()
	defer t.mu.Unlock()

	th := t.byID[id]
	if th == nil {
		return agent.Note{Conclusion: conclusion}
	}

	return agent.Note{
		Title:      th.about,
		Asked:      append([]string(nil), th.asked...),
		Conclusion: conclusion,
	}
}

// request describes a thread the way the agent needs it, and hands over the
// conclusions waiting on it: they travel with this one question, and a
// question that never arrives is a conclusion nobody needed to hear.
func (t *threads) request(id string) (agent.Thread, []agent.Note, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	th, err := t.openLocked(id)
	if err != nil {
		return agent.Thread{}, nil, err
	}

	notes := th.notes
	th.notes = nil

	return agent.Thread{ID: th.id, Parent: th.parent}, notes, nil
}

// finish closes a side thread, along with any side thread opened from it that
// is still going: a thread the user is done with is done with whatever hangs
// off it. It returns the thread itself first, then those, so the caller can
// tell each one's parent. The root thread cannot be finished.
func (t *threads) finish(id string) ([]thread, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	th, err := t.openLocked(id)
	if err != nil {
		return nil, err
	}

	if th.parent == "" {
		return nil, errors.New("the main thread cannot be finished")
	}

	th.closed = true
	closed := []thread{*th}

	// The tree is small and only ever grows, so a sweep per closing costs
	// nothing and saves keeping a list of children in step.
	for again := true; again; {
		again = false

		for _, other := range t.byID {
			if other.closed || other.parent == "" {
				continue
			}

			if parent := t.byID[other.parent]; parent != nil && parent.closed {
				other.closed = true
				closed = append(closed, *other)
				again = true
			}
		}
	}

	return closed, nil
}

// note leaves a finished side thread's conclusion for the thread above it.
func (t *threads) note(id string, n agent.Note) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	th := t.byID[id]
	if th == nil {
		return errNoThread
	}

	th.notes = append(th.notes, n)

	return nil
}

// title is what a thread is about, for logs and for what the agent is told.
func (t *threads) title(id string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if th := t.byID[id]; th != nil {
		return th.title
	}

	return ""
}

// openLocked finds a thread that can still be talked to. The caller holds t.mu.
func (t *threads) openLocked(id string) (*thread, error) {
	if id == "" {
		id = agent.RootThread
	}

	th := t.byID[id]
	if th == nil {
		return nil, errNoThread
	}

	if th.closed {
		return nil, errThreadDone
	}

	return th, nil
}

// trimTitle shortens a subject to one line of a sensible length.
func trimTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")

	if runes := []rune(title); len(runes) > threadTitle {
		return strings.TrimSpace(string(runes[:threadTitle])) + "…"
	}

	return title
}
