package speech

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

func TestResolveStyle(t *testing.T) {
	t.Parallel()

	if got := ResolveStyle(StyleDry); got != DryStyle {
		t.Errorf("dry preset = %q", got)
	}

	if got := ResolveStyle(StyleNone); got != "" {
		t.Errorf("none preset = %q", got)
	}

	if got := ResolveStyle("Read slowly."); got != "Read slowly." {
		t.Errorf("literal = %q", got)
	}
}

func TestSpeakInstructions(t *testing.T) {
	t.Parallel()

	persian := SpeakInstructions(DryStyle, lang.Lookup("fa"))
	if !strings.Contains(persian, "Persian (Farsi)") {
		t.Errorf("the voice was not told the language:\n%s", persian)
	}

	// The model speaks what follows the header, so it has to come last.
	if !strings.HasSuffix(persian, "Transcript:\n") {
		t.Errorf("instructions do not end with the transcript header:\n%s", persian)
	}

	// The note the voice is directed by sits under its own heading, where the
	// model looks for it.
	if want := "# Director's note\n" + DryStyle + "\nLanguage: "; !strings.Contains(persian, want) {
		t.Errorf("the director's note is not where it belongs:\n%s", persian)
	}

	if got := SpeakInstructions(DryStyle, lang.Lookup("xx")); !strings.HasSuffix(got, "Transcript:\n") ||
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
