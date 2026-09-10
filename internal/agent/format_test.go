package agent_test

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
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

// TestAnswerPromptHalfSpaces guards the escape the template is written with:
// the agent must receive real half-spaces, not the text \u200c, or the Persian
// examples in the prompt spell words that do not exist.
func TestAnswerPromptHalfSpaces(t *testing.T) {
	t.Parallel()

	if strings.Contains(agent.AnswerPrompt, `\u200c`) {
		t.Error("AnswerPrompt still carries the unreplaced escape text")
	}

	if !strings.Contains(agent.AnswerPrompt, "\u200c") {
		t.Error("AnswerPrompt has no half-space, so its Persian examples are misspelled")
	}
}
