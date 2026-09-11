package speech

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

func TestResolveStyle(t *testing.T) {
	t.Parallel()

	if got := ResolveStyle(StyleLecture); got != LectureStyle {
		t.Errorf("lecture preset = %q", got)
	}

	if got := ResolveStyle(StyleNone); got != "" {
		t.Errorf("none preset = %q", got)
	}

	if got := ResolveStyle("Read slowly."); got != "Read slowly.\n" {
		t.Errorf("literal = %q", got)
	}
}

func TestSpeakInstruction(t *testing.T) {
	t.Parallel()

	persian := SpeakInstruction(LectureStyle, lang.Lookup("fa"))
	if !strings.Contains(persian, "Persian (Farsi)") {
		t.Errorf("the voice was not told the language:\n%s", persian)
	}

	// The model speaks what follows the header, so it has to come last.
	if !strings.HasSuffix(persian, "Transcript:\n") {
		t.Errorf("instruction does not end with the transcript header:\n%s", persian)
	}

	if got := SpeakInstruction(LectureStyle, lang.Lookup("xx")); !strings.HasSuffix(got, "Transcript:\n") ||
		strings.Contains(got, "Language:") {
		t.Errorf("a language without a note should add none:\n%s", got)
	}

	// --tts-prompt=none means the text goes out on its own.
	if got := SpeakInstruction("", lang.Lookup("fa")); got != "" {
		t.Errorf("no style = %q", got)
	}
}

func TestTranscribeInstruction(t *testing.T) {
	t.Parallel()

	bare := TranscribeInstruction(lang.Lookup("xx"))
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

	hinted := TranscribeInstruction(lang.Lookup("fa"))
	if !strings.HasPrefix(hinted, TranscribePrompt) || !strings.Contains(hinted, "speaks Persian (Farsi)") {
		t.Errorf("hinted = %q", hinted)
	}
}

// A spoken summary has to sound like the spoken part of an answer, so it
// carries that language's register rules, and only that language's.
func TestSummarizeInstruction(t *testing.T) {
	t.Parallel()

	persian := SummarizeInstruction(lang.Lookup("fa"))
	if !strings.HasPrefix(persian, SummarizePrompt) || !strings.Contains(persian, "محاوره") {
		t.Errorf("the Persian instruction lost its register rules:\n%s", persian)
	}

	if english := SummarizeInstruction(lang.Lookup("en")); strings.Contains(english, "محاوره") {
		t.Errorf("the English instruction carries the Persian rules:\n%s", english)
	}

	if unknown := SummarizeInstruction(lang.Lookup("xx")); !strings.Contains(unknown, "the language of the passage") {
		t.Errorf("a language without rules lost its fallback:\n%s", unknown)
	}
}
