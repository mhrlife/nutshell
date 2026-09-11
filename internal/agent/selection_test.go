package agent_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestExcerptKeepsShortPassagesWhole(t *testing.T) {
	t.Parallel()

	passage := strings.Repeat("ب", 400)
	if got := agent.Excerpt("  " + passage + "\n"); got != passage {
		t.Errorf("a 400-character passage was cut: %q", got)
	}
}

// Characters, not bytes: a Persian passage cut by bytes would split letters
// in half and send the agent broken text.
func TestExcerptCutsLongPassagesInTheMiddle(t *testing.T) {
	t.Parallel()

	head := strings.Repeat("آ", 200)
	tail := strings.Repeat("z", 200)
	passage := head + strings.Repeat("middle ", 100) + tail

	got := agent.Excerpt(passage)

	if !strings.HasPrefix(got, head+"\n[…]\n") || !strings.HasSuffix(got, "\n"+tail) {
		t.Errorf("excerpt does not keep both edges:\n%s", got)
	}

	if strings.Contains(got, "middle") {
		t.Errorf("excerpt kept the middle:\n%s", got)
	}

	if !utf8.ValidString(got) {
		t.Errorf("excerpt split a character: %q", got)
	}
}

func TestMessage(t *testing.T) {
	t.Parallel()

	plain := agent.Request{Text: "why?", Selection: " \n "}
	if got := plain.Message(); got != "why?" {
		t.Errorf("a question without a selection changed: %q", got)
	}

	about := agent.Request{Text: "why?", Selection: "the cache is per process"}

	want := "<selected_text>\nthe cache is per process\n</selected_text>\n\nwhy?"
	if got := about.Message(); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}
