package server

// The page can be reloaded, closed, or lose the network at any moment, so the
// record of the conversation lives here rather than in the browser: every turn
// appends what it produces to a log, and a page catches up by replaying that
// log from wherever it left off.

import (
	"encoding/json"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
)

// Kinds of log entry. Everything the browser needs to draw a turn is one of these.
const (
	kindQuestion   = "question"
	kindTool       = "tool"
	kindText       = "text"
	kindPrompt     = "prompt"
	kindPromptDone = "prompt_done"
	kindResult     = "result"
	kindError      = "error"
	kindThread     = "thread"
	kindThreadDone = "thread_done"
)

// logEntry is one thing that happened, numbered so a client can say where it
// stopped reading. Thread is the conversation it happened in, so a browser
// showing one thread can tell which entries are its own.
type logEntry struct {
	Seq    int             `json:"seq"`
	Turn   int             `json:"turn"`
	Thread string          `json:"thread"`
	Kind   string          `json:"kind"`
	Data   json.RawMessage `json:"data"`
}

// session is the append-only record of the conversation.
type session struct {
	logger *slog.Logger

	mu      sync.Mutex // guards the fields below
	entries []logEntry
	seq     int
	turns   int
	changed chan struct{} // closed on every append, then replaced by the next one
}

func newSession(logger *slog.Logger) *session {
	return &session{logger: logger, changed: make(chan struct{})}
}

// startTurn records a question asked on one thread, with the excerpt of the
// passage it is about when there is one, and returns the turn number the rest
// of that turn's entries carry. Turns are numbered across all threads at
// once, so a number names one turn and nothing else.
func (s *session) startTurn(q question) int {
	s.mu.Lock()
	s.turns++
	turn := s.turns
	s.mu.Unlock()

	entry := map[string]any{"text": q.text, "started": time.Now().UnixMilli()}
	if q.selection != "" {
		entry["selection"] = q.selection
	}

	if q.closing {
		entry["closing"] = true
	}

	s.add(q.thread, turn, kindQuestion, entry)

	return turn
}

// question is what starts a turn.
type question struct {
	thread    string
	text      string
	selection string // the passage the question is about, already shortened
	// closing marks nutshell's own question rather than the user's: the one
	// that asks a side thread what it settled before it is finished.
	closing bool
}

// mark records something that happened to a thread rather than inside a turn:
// a side thread opened, or finished. Both are written to the log of the
// thread above, which is where the user is when they happen.
func (s *session) mark(thread, kind string, data any) {
	s.add(thread, 0, kind, data)
}

// add appends one entry and wakes every reader waiting for it.
func (s *session) add(thread string, turn int, kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("recording a session entry", "turn", turn, "kind", kind, "error", err)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	entry := logEntry{Seq: s.seq, Turn: turn, Thread: thread, Kind: kind, Data: raw}

	// Tool and text entries are the turn's live activity line, of which only
	// the newest is ever on screen: replacing the previous one keeps a long
	// turn from filling the log with lines nobody will read again.
	if last := len(s.entries) - 1; last >= 0 && supersedes(entry, s.entries[last]) {
		s.entries[last] = entry
	} else {
		s.entries = append(s.entries, entry)
	}

	close(s.changed)
	s.changed = make(chan struct{})
}

func supersedes(entry, previous logEntry) bool {
	return isActivity(entry.Kind) && isActivity(previous.Kind) && entry.Turn == previous.Turn
}

func isActivity(kind string) bool { return kind == kindTool || kind == kindText }

// since returns the entries numbered after seq, along with a channel that is
// closed once there is more, so a reader can wait instead of polling.
func (s *session) since(seq int) ([]logEntry, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	first := sort.Search(len(s.entries), func(i int) bool { return s.entries[i].Seq > seq })

	return slices.Clone(s.entries[first:]), s.changed
}
