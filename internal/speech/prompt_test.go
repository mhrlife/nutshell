package speech

import (
	"strings"
	"testing"
)

func TestResolvePrompt(t *testing.T) {
	t.Parallel()

	if got := ResolvePrompt(PromptLecture); got != LecturePrompt || !strings.HasSuffix(got, "Transcript:\n") {
		t.Errorf("lecture preset = %q", got)
	}

	if got := ResolvePrompt(PromptNone); got != "" {
		t.Errorf("none preset = %q", got)
	}

	if got := ResolvePrompt("Read slowly."); got != "Read slowly.\n" {
		t.Errorf("literal = %q", got)
	}
}

func TestTranscribeInstruction(t *testing.T) {
	t.Parallel()

	bare := TranscribeInstruction("")
	if bare != TranscribePrompt {
		t.Errorf("no hint = %q", bare)
	}

	// The rule that keeps English terms in Latin script is the point of the
	// prompt; losing it silently turns tool names into gibberish.
	for _, want := range []string{"letters of another script", "AskUserQuestion", "loanwords included"} {
		if !strings.Contains(bare, want) {
			t.Errorf("prompt lost %q:\n%s", want, bare)
		}
	}

	hinted := TranscribeInstruction("Persian (Farsi)")
	if !strings.HasPrefix(hinted, TranscribePrompt) || !strings.HasSuffix(hinted, "speaks Persian (Farsi).") {
		t.Errorf("hinted = %q", hinted)
	}
}
