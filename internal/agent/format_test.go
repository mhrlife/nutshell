package agent_test

import (
	"encoding/json"
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

// The instructions must carry the rules of the language in front of them and
// no other: the register rules only work when they are not competing with a
// second language's.
func TestInstructionsCarryOneLanguage(t *testing.T) {
	t.Parallel()

	persian := agent.Instructions(lang.Lookup("fa"), agent.Tagged)
	if !strings.Contains(persian, "Persian (Farsi)") || !strings.Contains(persian, "محاوره") {
		t.Errorf("the Persian instructions lost their register rules:\n%s", persian)
	}

	english := agent.Instructions(lang.Lookup("en"), agent.Tagged)
	if strings.Contains(english, "محاوره") {
		t.Errorf("the English instructions carry the Persian rules:\n%s", english)
	}

	unknown := agent.Instructions(lang.Lookup("xx"), agent.Tagged)
	if !strings.Contains(unknown, "the language the user spoke") {
		t.Errorf("a language without rules lost its fallback:\n%s", unknown)
	}

	// Whatever the language, the reply format is the point of the instructions.
	for _, got := range []string{persian, english, unknown} {
		if !strings.Contains(got, "<summary>") || !strings.Contains(got, "<full>") {
			t.Errorf("instructions lost the answer format:\n%s", got)
		}
	}
}

// Structured instructions name the two fields and never the tags: a model
// told about both writes tags into its fields.
func TestStructuredInstructionsNameTheFields(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"fa", "en", "xx"} {
		got := agent.Instructions(lang.Lookup(code), agent.Structured)
		if strings.Contains(got, "<summary>") || strings.Contains(got, "<full>") {
			t.Errorf("%s: structured instructions mention the tags:\n%s", code, got)
		}

		if !strings.Contains(got, "structured output") {
			t.Errorf("%s: structured instructions never say how the reply is given:\n%s", code, got)
		}
	}
}

func TestAnswerSchemaIsValidJSON(t *testing.T) {
	t.Parallel()

	var schema struct {
		Required []string `json:"required"`
	}

	if err := json.Unmarshal([]byte(agent.AnswerSchema), &schema); err != nil {
		t.Fatalf("AnswerSchema is not JSON: %v", err)
	}

	if strings.Join(schema.Required, ",") != "summary,full" {
		t.Errorf("required = %v, want [summary full]", schema.Required)
	}
}

func TestDecodeAnswer(t *testing.T) {
	t.Parallel()

	raw := `{"summary":"Short.","full":"# Long"}`

	got, err := agent.DecodeAnswer(raw, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("DecodeAnswer: %v", err)
	}

	if got.Summary != "Short." || got.Full != "# Long" || got.Raw != raw {
		t.Errorf("answer = %+v", got)
	}

	if _, err := agent.DecodeAnswer("", json.RawMessage(`[1]`)); err == nil {
		t.Error("DecodeAnswer accepted something that is not an object")
	}
}
