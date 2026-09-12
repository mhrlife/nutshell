package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
)

// The whole point of a side thread: it starts as a copy of the conversation
// it came from, what is asked in it never reaches that conversation, and only
// the conclusion travels back — on the next question, and only once.
func TestSideThreadCarriesItsConclusionUp(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "a log written before the change", Full: "F"}, asked: make(chan agent.Request, 4)}

	ts := newTestServer(ag)
	defer ts.Close()

	stream, drop := follow(t, ts.URL)
	defer drop()

	stream.done(t, ask(t, ts.URL, `{"text":"how does it survive a crash?"}`))
	asked(t, ag)

	forked := ask(t, ts.URL, `{"text":"what is that?","selection":"the write-ahead log","fork":true}`)
	if forked.Thread != "t1" {
		t.Fatalf("the side thread is %q, want t1", forked.Thread)
	}

	side := asked(t, ag)
	if side.Thread.ID != "t1" || side.Thread.Parent != agent.RootThread {
		t.Errorf("the question was asked on %+v, want t1 forked from the main thread", side.Thread)
	}

	opened := stream.await(t, `"thread":"root","kind":"thread"`)
	if !strings.Contains(opened, "the write-ahead log") {
		t.Errorf("the main thread was not told what the side thread is about:\n%s", opened)
	}

	stream.done(t, forked)

	// Closing it asks the side thread what it settled, then finishes it.
	closing := post(t, ts.URL+"/api/close", `{"thread":"t1","inject":true}`)
	defer closing.Body.Close()

	if closing.StatusCode != http.StatusAccepted {
		t.Fatalf("close status = %d, want %d", closing.StatusCode, http.StatusAccepted)
	}

	if conclusion := asked(t, ag); conclusion.Text != agent.Conclusion() {
		t.Errorf("the side thread was asked %q, want it to sum itself up", conclusion.Text)
	}

	done := stream.await(t, `"thread":"root","kind":"thread_done"`)
	if !strings.Contains(done, `"injected":true`) || !strings.Contains(done, ag.answer.Summary) {
		t.Errorf("the main thread was not told what came of it:\n%s", done)
	}

	// The next question on the main thread carries it, and only that one.
	carried := ask(t, ts.URL, `{"text":"so where do we put it?"}`)

	back := asked(t, ag)
	checkNote(t, back, ag.answer.Summary)

	stream.done(t, carried)
	ask(t, ts.URL, `{"text":"and then?"}`)

	if again := asked(t, ag); len(again.Notes) != 0 {
		t.Errorf("the conclusion travelled twice: %+v", again.Notes)
	}
}

func TestClosedSideThreadTakesNoMoreQuestions(t *testing.T) {
	t.Parallel()

	ag := &fakeAgent{answer: agent.Answer{Summary: "S"}, asked: make(chan agent.Request, 2)}

	ts := newTestServer(ag)
	defer ts.Close()

	stream, drop := follow(t, ts.URL)
	defer drop()

	forked := ask(t, ts.URL, `{"text":"what is that?","fork":true}`)
	asked(t, ag)
	stream.done(t, forked)

	closed := post(t, ts.URL+"/api/close", `{"thread":"`+forked.Thread+`"}`)
	defer closed.Body.Close()

	if closed.StatusCode != http.StatusNoContent {
		t.Fatalf("close status = %d, want %d", closed.StatusCode, http.StatusNoContent)
	}

	resp := post(t, ts.URL+"/api/ask", `{"text":"one more","thread":"`+forked.Thread+`"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("asking a finished side thread: status = %d, want 400", resp.StatusCode)
	}

	// And the turn lock was handed back, so the main thread still works.
	ask(t, ts.URL, `{"text":"back here"}`)
}

func TestAskRejectsAThreadNobodyOpened(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	defer ts.Close()

	resp := post(t, ts.URL+"/api/ask", `{"text":"hi","thread":"t9"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// checkNote reads what the finished side thread left on this question: the
// passage it was opened from, the question that was put to it, and what it
// settled — a conclusion on its own would not say what was being asked.
func checkNote(t *testing.T, req agent.Request, conclusion string) {
	t.Helper()

	if len(req.Notes) != 1 || req.Notes[0].Conclusion != conclusion {
		t.Fatalf("the conclusion never reached the main thread: %+v", req.Notes)
	}

	if got := req.Notes[0].Asked; len(got) != 1 || got[0] != "what is that?" {
		t.Errorf("the question asked in the side thread did not travel: %q", got)
	}

	if req.Notes[0].Title != "the write-ahead log" {
		t.Errorf("the note is about %q, want the passage it was opened from", req.Notes[0].Title)
	}

	for _, want := range []string{"<side_thread", "<asked>\nwhat is that?\n</asked>", "<settled>"} {
		if !strings.Contains(req.Message(), want) {
			t.Errorf("the message is missing %q:\n%s", want, req.Message())
		}
	}
}

// started is what /api/ask says about the turn it just set going.
type started struct {
	Turn   int    `json:"turn"`
	Thread string `json:"thread"`
}

// ask posts a question and fails the test unless it was accepted.
func ask(t *testing.T, base, body string) started {
	t.Helper()

	resp := post(t, base+"/api/ask", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("ask status = %d: %s", resp.StatusCode, out)
	}

	var out started
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("reading the ask reply: %v", err)
	}

	return out
}

// asked is the next request to reach the agent.
func asked(t *testing.T, ag *fakeAgent) agent.Request {
	t.Helper()

	select {
	case req := <-ag.asked:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was never asked")

		return agent.Request{}
	}
}

// follower reads one event stream and keeps what it has already seen, so a
// test can wait for several things in a row without losing what arrived
// between two waits.
type follower struct {
	body io.Reader
	seen string
}

func follow(t *testing.T, base string) (*follower, func()) {
	t.Helper()

	body, drop := openStream(t, base, 0)

	return &follower{body: body}, drop
}

// await reads until the stream has carried want, and returns everything seen.
func (f *follower) await(t *testing.T, want string) string {
	t.Helper()

	if strings.Contains(f.seen, want) {
		return f.seen
	}

	read := make(chan string, 1)

	go func() {
		var seen strings.Builder

		seen.WriteString(f.seen)

		buf := make([]byte, 4096)

		for {
			n, err := f.body.Read(buf)
			seen.Write(buf[:n])

			if strings.Contains(seen.String(), want) || err != nil {
				read <- seen.String()

				return
			}
		}
	}()

	select {
	case got := <-read:
		f.seen = got

		if !strings.Contains(got, want) {
			t.Fatalf("stream missing %q:\n%s", want, got)
		}

		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q:\n%s", want, f.seen)

		return ""
	}
}

// done waits for the answer to one turn, so the next question is not turned
// away for arriving while the agent is still busy.
func (f *follower) done(t *testing.T, s started) string {
	t.Helper()

	return f.await(t, fmt.Sprintf(`"turn":%d,"thread":%q,"kind":"result"`, s.Turn, s.Thread))
}
