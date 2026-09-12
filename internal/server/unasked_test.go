package server_test

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

// The bug this covers: the agent answered once to say it had started work in
// the background, and a second time when that work finished. The second answer
// belonged to no question, so nothing on screen ever showed it — the user was
// promised a result and never heard it.
func TestAnswerWithNoQuestionGetsATurnOfItsOwn(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "the migration is running", Full: "F"}}

	ts := newTestServer(ag)
	defer ts.Close()

	stream, drop := follow(t, ts.URL)
	defer drop()

	stream.done(t, ask(t, ts.URL, `{"text":"start the migration in the background"}`))

	ag.speakUp(agent.Thread{}, agent.Answer{Summary: "the migration finished, 412 rows moved", Full: "all of it"}, nil)

	opened := stream.await(t, `"turn":2,"thread":"root","kind":"question"`)
	if !strings.Contains(opened, `"unasked":true`) {
		t.Errorf("the turn does not say nobody asked for it:\n%s", opened)
	}

	// It carries the agent's progress and its answer like any other turn, which
	// is what makes the browser draw it and read it aloud.
	result := stream.await(t, `"turn":2,"thread":"root","kind":"result"`)
	if !strings.Contains(result, "412 rows moved") || !strings.Contains(result, "the task output") {
		t.Errorf("the answer nobody asked for never reached the stream:\n%s", result)
	}
}

// A turn of the agent's own that failed still has to end, or the browser waits
// on a turn that is never coming.
func TestAnswerWithNoQuestionReportsItsFailure(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "S", Full: "F"}}

	ts := newTestServer(ag)
	defer ts.Close()

	stream, drop := follow(t, ts.URL)
	defer drop()

	ag.speakUp(agent.Thread{ID: "t1"}, agent.Answer{}, agent.ErrCancelled)

	failed := stream.await(t, `"kind":"error"`)
	if !strings.Contains(failed, `"thread":"t1"`) || !strings.Contains(failed, `"code":"cancelled"`) {
		t.Errorf("a cancelled turn of the agent's own was not reported as one:\n%s", failed)
	}
}
