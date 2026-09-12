package claudecode

import (
	"slices"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestHistoryArgs(t *testing.T) {
	t.Parallel()

	const (
		rootSession = "abc"
		sideSession = "def"
	)

	sessions := map[string]string{agent.RootThread: rootSession, "t1": sideSession}

	tests := []struct {
		name     string
		sessions map[string]string
		thread   agent.Thread
		want     []string
	}{
		{"nothing said yet", map[string]string{}, agent.Thread{}, nil},
		{"the main thread resumes itself", sessions, agent.Thread{}, []string{resumeFlag, rootSession}},
		{
			"a side thread resumes itself",
			sessions,
			agent.Thread{ID: "t1", Parent: agent.RootThread},
			[]string{resumeFlag, sideSession},
		},
		{
			"a side thread's first question forks its parent",
			sessions,
			agent.Thread{ID: "t2", Parent: agent.RootThread},
			[]string{resumeFlag, rootSession, "--fork-session"},
		},
		{
			"a side thread of a thread that never spoke starts fresh",
			sessions,
			agent.Thread{ID: "t3", Parent: "t2"},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := historyArgs(tt.sessions, tt.thread); !slices.Equal(got, tt.want) {
				t.Errorf("historyArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetSessionIDBelongsToItsThread(t *testing.T) {
	t.Parallel()

	a := New("claude", nil, nil)

	a.thread = agent.RootThread
	a.setSessionID("abc")

	a.thread = "t1" // the fork's own session, which must not land on the parent
	a.setSessionID("def")

	if got := a.SessionID(agent.Thread{}); got != "abc" {
		t.Errorf("the main thread's session is %q, want abc", got)
	}

	if got := a.SessionID(agent.Thread{ID: "t1"}); got != "def" {
		t.Errorf("the side thread's session is %q, want def", got)
	}
}
