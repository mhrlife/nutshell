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
)

// logEntry is one thing that happened, numbered so a client can say where it
// stopped reading.
type logEntry struct {
	Seq  int             `json:"seq"`
	Turn int             `json:"turn"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
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

// startTurn records a question, with the excerpt of the passage it is about
// when there is one, and returns the turn number the rest of that turn's
// entries carry.
func (s *session) startTurn(question, selection string) int {
	s.mu.Lock()
	s.turns++
	turn := s.turns
	s.mu.Unlock()

	entry := map[string]any{"text": question, "started": time.Now().UnixMilli()}
	if selection != "" {
		entry["selection"] = selection
	}

	s.add(turn, kindQuestion, entry)

	return turn
}

// add appends one entry and wakes every reader waiting for it.
func (s *session) add(turn int, kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("recording a session entry", "turn", turn, "kind", kind, "error", err)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	entry := logEntry{Seq: s.seq, Turn: turn, Kind: kind, Data: raw}

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
