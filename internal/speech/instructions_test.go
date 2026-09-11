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

func TestSpeakInstructions(t *testing.T) {
	t.Parallel()

	persian := SpeakInstructions(LectureStyle, lang.Lookup("fa"))
	if !strings.Contains(persian, "Persian (Farsi)") {
		t.Errorf("the voice was not told the language:\n%s", persian)
	}

	// The model speaks what follows the header, so it has to come last.
	if !strings.HasSuffix(persian, "Transcript:\n") {
		t.Errorf("instructions do not end with the transcript header:\n%s", persian)
	}

	if got := SpeakInstructions(LectureStyle, lang.Lookup("xx")); !strings.HasSuffix(got, "Transcript:\n") ||
		strings.Contains(got, "Language:") {
		t.Errorf("a language without a note should add none:\n%s", got)
	}

	// --tts-prompt=none means the text goes out on its own.
	if got := SpeakInstructions("", lang.Lookup("fa")); got != "" {
		t.Errorf("no style = %q", got)
	}
}

func TestTranscribeInstructions(t *testing.T) {
	t.Parallel()

	bare := TranscribeInstructions(lang.Lookup("xx"))
	if bare != transcribeTask {
		t.Errorf("no hint = %q", bare)
	}

	// The rule that keeps English terms in Latin script is the point of the
	// instructions; losing it silently turns tool names into gibberish.
	for _, want := range []string{"letters of another script", "AskUserQuestion", "loanwords included"} {
		if !strings.Contains(bare, want) {
			t.Errorf("instructions lost %q:\n%s", want, bare)
		}
	}

	hinted := TranscribeInstructions(lang.Lookup("fa"))
	if !strings.HasPrefix(hinted, transcribeTask) || !strings.Contains(hinted, "speaks Persian (Farsi)") {
		t.Errorf("hinted = %q", hinted)
	}
}

// A spoken summary has to sound like the spoken part of an answer, so it
// carries that language's register rules, and only that language's.
func TestSummarizeInstructions(t *testing.T) {
	t.Parallel()

	persian := SummarizeInstructions(lang.Lookup("fa"))
	if !strings.HasPrefix(persian, summarizeTask) || !strings.Contains(persian, "محاوره") {
		t.Errorf("the Persian instructions lost their register rules:\n%s", persian)
	}

	if english := SummarizeInstructions(lang.Lookup("en")); strings.Contains(english, "محاوره") {
		t.Errorf("the English instructions carry the Persian rules:\n%s", english)
	}

	if unknown := SummarizeInstructions(lang.Lookup("xx")); !strings.Contains(unknown, "the language of the passage") {
		t.Errorf("a language without rules lost its fallback:\n%s", unknown)
	}
}
