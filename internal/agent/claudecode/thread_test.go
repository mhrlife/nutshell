package claudecode

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

const (
	rootSession = "sess-root"
	sideSession = "sess-side"
)

func TestHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sessions map[string]string
		thread   agent.Thread
		want     string
		wantFork bool
	}{
		{"nothing said yet", map[string]string{}, agent.Thread{}, "", false},
		{
			"the main thread resumes itself",
			map[string]string{agent.RootThread: rootSession},
			agent.Thread{},
			rootSession,
			false,
		},
		{
			"a side thread resumes itself",
			map[string]string{agent.RootThread: rootSession, "t1": sideSession},
			agent.Thread{ID: "t1", Parent: agent.RootThread},
			sideSession,
			false,
		},
		{
			"a side thread's first question forks its parent",
			map[string]string{agent.RootThread: rootSession},
			agent.Thread{ID: "t1", Parent: agent.RootThread},
			rootSession,
			true,
		},
		{
			"a side thread of a thread that never spoke starts fresh",
			map[string]string{agent.RootThread: rootSession},
			agent.Thread{ID: "t2", Parent: "t1"},
			"",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := New([]string{claudeBin}, DirectLaunch, nil, slog.Default())
			a.sessions = tt.sessions
			a.thread = tt.thread

			id, fork := a.history()
			if id != tt.want || fork != tt.wantFork {
				t.Errorf("history() = %q, %v, want %q, %v", id, fork, tt.want, tt.wantFork)
			}
		})
	}
}

// Forking is Claude Code's own doing, so the flag belongs after the separator
// even though the session it applies to was handed to the wrapper host.
func TestCommandForksASideThread(t *testing.T) {
	t.Parallel()

	for _, launch := range []Launch{DirectLaunch, WrapperLaunch} {
		a := New([]string{"host"}, launch, nil, slog.Default())
		a.sessions[agent.RootThread] = rootSession
		a.thread = agent.Thread{ID: "t1", Parent: agent.RootThread}

		got := a.command()
		if !slices.Contains(got, "--fork-session") || !slices.Contains(got, rootSession) {
			t.Fatalf("launch %d did not fork the thread it came from: %v", launch, got)
		}

		if launch == WrapperLaunch && indexOf(got, "--fork-session") < indexOf(got, separator) {
			t.Errorf("wrapper launch put the fork flag before the separator: %v", got)
		}
	}
}

// A thread's session is its own: the fork's id must not land on its parent.
func TestSessionIDBelongsToItsThread(t *testing.T) {
	t.Parallel()

	a := New([]string{claudeBin}, DirectLaunch, nil, slog.Default())

	a.thread = agent.Thread{}
	a.setSessionID(rootSession)

	a.thread = agent.Thread{ID: "t1", Parent: agent.RootThread}
	a.setSessionID(sideSession)

	if got := a.SessionID(agent.Thread{}); got != rootSession {
		t.Errorf("the main thread's session is %q, want %q", got, rootSession)
	}

	if got := a.SessionID(agent.Thread{ID: "t1"}); got != sideSession {
		t.Errorf("the side thread's session is %q, want %q", got, sideSession)
	}
}
