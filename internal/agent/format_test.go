package agent_test

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

func TestParseAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		wantSummary string
		wantFull    string
	}{
		{
			name:        "tagged reply",
			raw:         "<summary>\nShort.\n</summary>\n<full>\n# Long\n\nDetail.\n</full>",
			wantSummary: "Short.",
			wantFull:    "# Long\n\nDetail.",
		},
		{
			name:        "untagged reply falls back to the whole text",
			raw:         "Just prose.",
			wantSummary: "Just prose.",
			wantFull:    "Just prose.",
		},
		{
			name:        "summary only",
			raw:         "<summary>Only this.</summary>",
			wantSummary: "Only this.",
			wantFull:    "Only this.",
		},
		{
			name:        "full only",
			raw:         "<full>All of it.</full>",
			wantSummary: "All of it.",
			wantFull:    "All of it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := agent.ParseAnswer(tt.raw)
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}

			if got.Full != tt.wantFull {
				t.Errorf("Full = %q, want %q", got.Full, tt.wantFull)
			}

			if got.Raw != tt.raw {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.raw)
			}
		})
	}
}

// The prompt must carry the rules of the language in front of it and no
// other: the register rules only work when they are not competing with a
// second language's.
func TestAnswerPromptCarriesOneLanguage(t *testing.T) {
	t.Parallel()

	persian := agent.AnswerPrompt(lang.Lookup("fa"))
	if !strings.Contains(persian, "Persian (Farsi)") || !strings.Contains(persian, "محاوره") {
		t.Errorf("the Persian prompt lost its register rules:\n%s", persian)
	}

	english := agent.AnswerPrompt(lang.Lookup("en"))
	if strings.Contains(english, "محاوره") {
		t.Errorf("the English prompt carries the Persian rules:\n%s", english)
	}

	unknown := agent.AnswerPrompt(lang.Lookup("xx"))
	if !strings.Contains(unknown, "the language the user spoke") {
		t.Errorf("a language without rules lost its fallback:\n%s", unknown)
	}

	// Whatever the language, the reply format is the point of the prompt.
	for _, got := range []string{persian, english, unknown} {
		if !strings.Contains(got, "<summary>") || !strings.Contains(got, "<full>") {
			t.Errorf("prompt lost the answer format:\n%s", got)
		}
	}
}
