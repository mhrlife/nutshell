package codex

import (
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestRecoverProvisionalThread(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		thread agent.Thread
		prior  conversation
		want   string
	}{
		{name: "missing root", prior: conversation{id: testMissing, provisional: true}, want: "thread/start:root"},
		{
			name: "missing fork", thread: agent.Thread{ID: testSide, Parent: agent.RootThread},
			prior: conversation{id: testMissing, provisional: true}, want: "thread/fork:side",
		},
		{name: "existing provisional history", prior: conversation{id: agent.RootThread, provisional: true}, want: testResumeRoot},
		{name: "confirmed history missing", prior: conversation{id: testMissing}},
		{name: "other resume error", prior: conversation{id: "unavailable", provisional: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := testAgent(t)
			a.sessions[agent.RootThread] = conversation{id: agent.RootThread}
			a.sessions[tc.thread.Name()] = tc.prior

			answer, err := a.Ask(testContext(t), agent.Request{Text: testAnswer, Thread: tc.thread},
				agent.ProgressFunc(func(agent.Event) {}))
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected resume error")
				}

				if a.sessions[tc.thread.Name()] != tc.prior {
					t.Fatal("lost history mapping on resume error")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if answer.Summary != tc.want {
				t.Fatalf("answer = %q, want %q", answer.Summary, tc.want)
			}
		})
	}
}
