package agent_test

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestThreadName(t *testing.T) {
	t.Parallel()

	if got := (agent.Thread{}).Name(); got != agent.RootThread {
		t.Errorf("the zero thread is %q, want %q", got, agent.RootThread)
	}

	if !(agent.Thread{ID: agent.RootThread}).Root() {
		t.Error("the root thread does not call itself the root")
	}

	if (agent.Thread{ID: "t1", Parent: agent.RootThread}).Root() {
		t.Error("a side thread calls itself the root")
	}
}

func TestMessageCarriesNotes(t *testing.T) {
	t.Parallel()

	req := agent.Request{
		Text:      "so where do we put it?",
		Selection: "the write-ahead log",
		Notes: []agent.Note{
			{
				Title:      "what is a write-ahead log",
				Asked:      []string{"what is that?", "and why before the change?"},
				Conclusion: "a log written before the change it describes.",
			},
			{Title: "  ", Conclusion: "  "}, // nothing was settled: nothing to carry
		},
	}

	msg := req.Message()

	for _, want := range []string{
		`<side_thread subject="what is a write-ahead log">`,
		"<asked>\nwhat is that?\n</asked>",
		"<asked>\nand why before the change?\n</asked>",
		"<settled>\na log written before the change it describes.\n</settled>",
		"</side_thread>",
		"<selected_text>",
		"the write-ahead log",
		"so where do we put it?",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q:\n%s", want, msg)
		}
	}

	if strings.Count(msg, "<side_thread") != 1 {
		t.Errorf("an empty conclusion was carried anyway:\n%s", msg)
	}

	if notes, selection := strings.Index(msg, "<side_thread"), strings.Index(msg, "<selected_text>"); notes > selection {
		t.Errorf("the notes come after the passage:\n%s", msg)
	}
}

func TestMessageWithoutNotesIsUnchanged(t *testing.T) {
	t.Parallel()

	req := agent.Request{Text: "what is a bloom filter?"}

	if got := req.Message(); got != req.Text {
		t.Errorf("Message() = %q, want the question itself", got)
	}
}

func TestConclusionAsksForASummary(t *testing.T) {
	t.Parallel()

	if !strings.Contains(agent.Conclusion(), "<summary>") {
		t.Error("the closing question never mentions the part that travels back")
	}
}
