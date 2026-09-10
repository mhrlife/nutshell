package server

// One server-sent-event stream carries the whole conversation to the browser.
// It is deliberately not the reply to a question: a turn runs on the server
// and a page merely watches it, so reloading the page — even mid-turn — costs
// nothing but a reconnection.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	// heartbeat keeps idle connections from being dropped by the browser or
	// anything sitting between it and us.
	heartbeat = 15 * time.Second
	// reconnectAfter is how long the browser should wait before coming back.
	reconnectAfter = 2 * time.Second
)

// handleStream replays the conversation from where this client stopped
// reading and then follows it live.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported by this connection"))

		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "retry: %d\n\n", reconnectAfter.Milliseconds())

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	cursor := resumePoint(r)
	caughtUp := false

	for {
		entries, more := s.session.since(cursor)
		for _, entry := range entries {
			if line, err := json.Marshal(entry); err == nil {
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", entry.Seq, line)
			}

			cursor = entry.Seq
		}

		if !caughtUp { // the backlog is on screen; whatever follows is news
			caughtUp = true

			fmt.Fprint(w, "event: synced\ndata: {}\n\n")
		}

		flusher.Flush()

		select {
		case <-more:
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// resumePoint is the last entry this client saw. A reconnecting EventSource
// tells us in a header; a freshly loaded page asks for everything.
func resumePoint(r *http.Request) int {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("from")
	}

	seq, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}

	return seq
}
