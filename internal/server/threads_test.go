package server

import (
	"errors"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestThreadsForkAndFinish(t *testing.T) {
	t.Parallel()

	tree := newThreads()

	id, err := tree.fork(agent.RootThread, "  what is a\nwrite-ahead log  ", "how does that work?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	if got := tree.title(id); got != "what is a write-ahead log" {
		t.Errorf("title = %q, want the subject on one line", got)
	}

	deeper, err := tree.fork(id, "", "and what is fsync?")
	if err != nil {
		t.Fatalf("forking a side thread: %v", err)
	}

	// Nothing was selected there, so the question itself says what it is about.
	if got := tree.title(deeper); got != "and what is fsync?" {
		t.Errorf("title = %q, want the question that opened it", got)
	}

	closed, err := tree.finish(deeper)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	if len(closed) != 1 || closed[0].parent != id {
		t.Errorf("finishing the deepest thread closed %+v", closed)
	}

	if _, _, err := tree.request(deeper); !errors.Is(err, errThreadDone) {
		t.Errorf("a finished thread still takes questions: %v", err)
	}
}

// A thread the user is done with is done with whatever hangs off it: anything
// still open below goes too, rather than being left where nothing can reach it.
func TestThreadsFinishTakesWhatHangsBelow(t *testing.T) {
	t.Parallel()

	tree := newThreads()

	top, err := tree.fork(agent.RootThread, "durability", "how is it kept?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	middle, err := tree.fork(top, "fsync", "what does it do?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	bottom, err := tree.fork(middle, "page cache", "and that?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	closed, err := tree.finish(top)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	if len(closed) != 3 || closed[0].id != top {
		t.Fatalf("finishing %q closed %+v, want it and the two below, itself first", top, closed)
	}

	for _, id := range []string{top, middle, bottom} {
		if _, _, err := tree.request(id); !errors.Is(err, errThreadDone) {
			t.Errorf("%q still takes questions: %v", id, err)
		}
	}
}

func TestThreadsRootCannotBeFinished(t *testing.T) {
	t.Parallel()

	tree := newThreads()

	if _, err := tree.finish(agent.RootThread); err == nil {
		t.Error("the main thread was finished")
	}

	if _, err := tree.fork("t9", "nowhere", "anyone there?"); !errors.Is(err, errNoThread) {
		t.Errorf("forking from a thread nobody opened: %v", err)
	}
}

// A conclusion on its own says what was settled but not what was being
// asked, so the questions put to a side thread travel with it.
func TestThreadsConcludeCarriesTheQuestions(t *testing.T) {
	t.Parallel()

	tree := newThreads()

	id, err := tree.fork(agent.RootThread, "the write-ahead log", "what is that?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	tree.record(id, "what is that?")
	tree.record(id, "and why before the change?")

	note := tree.conclude(id, "a log written before the change it describes.")

	if note.Title != "the write-ahead log" {
		t.Errorf("subject = %q, want the passage it was opened from", note.Title)
	}

	if len(note.Asked) != 2 || note.Asked[0] != "what is that?" || note.Asked[1] != "and why before the change?" {
		t.Errorf("questions = %q, want both, in the order they were asked", note.Asked)
	}
}

func TestThreadsNotesTravelOnce(t *testing.T) {
	t.Parallel()

	tree := newThreads()
	note := agent.Note{Title: "fsync", Conclusion: "it blocks until the disk says so."}

	if err := tree.note(agent.RootThread, note); err != nil {
		t.Fatalf("note: %v", err)
	}

	// A side thread opened before the conclusion was passed on starts as a
	// copy of a conversation that has not heard it either, so it takes one too.
	id, err := tree.fork(agent.RootThread, "durability", "how is it kept?")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	th, notes, err := tree.request(id)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if th.Parent != agent.RootThread || len(notes) != 1 || notes[0].Conclusion != note.Conclusion {
		t.Fatalf("the side thread was handed %+v on %+v", notes, th)
	}

	if _, notes, _ = tree.request(id); len(notes) != 0 {
		t.Errorf("the conclusion travelled twice: %+v", notes)
	}

	if _, notes, _ = tree.request(agent.RootThread); len(notes) != 1 {
		t.Errorf("the thread it was left for never heard it: %+v", notes)
	}
}
