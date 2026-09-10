package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestDescribeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bash command", `{"command":"ls -la","description":"list"}`, "ls -la"},
		{"file path", `{"file_path":"/tmp/x.go"}`, "/tmp/x.go"},
		{"newlines collapsed", `{"command":"a\nb"}`, "a b"},
		{"unknown keys fall back to json", `{"foo":"bar"}`, `{"foo":"bar"}`},
		{"empty", `{}`, `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := describeInput(json.RawMessage(tt.input)); got != tt.want {
				t.Errorf("describeInput(%s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDescribeInputTruncates(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 300)
	got := describeInput(json.RawMessage(`{"command":"` + long + `"}`))

	if !strings.HasSuffix(got, "…") || len([]rune(got)) != maxDetailRunes+1 {
		t.Errorf("got %d runes, want %d plus ellipsis", len([]rune(got)), maxDetailRunes)
	}
}

func TestHandleEventReportsProgress(t *testing.T) {
	t.Parallel()

	var events []agent.Event

	progress := func(e agent.Event) { events = append(events, e) }

	assistant := streamEvent{
		Type:    "assistant",
		Message: json.RawMessage(`{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},{"type":"text","text":"Looking."}]}`),
	}
	if _, done, err := handleEvent(assistant, progress); done || err != nil {
		t.Fatalf("assistant event: done=%v err=%v", done, err)
	}

	if len(events) != 2 || events[0].Tool != "Read" || events[0].Detail != "a.go" || events[1].Text != "Looking." {
		t.Fatalf("unexpected progress events: %+v", events)
	}
}

func TestHandleEventResult(t *testing.T) {
	t.Parallel()

	progress := func(agent.Event) {}
	result := streamEvent{Type: "result", Result: "<summary>S</summary><full>F</full>", TotalCostUSD: 0.5}

	answer, done, err := handleEvent(result, progress)
	if !done || err != nil {
		t.Fatalf("result event: done=%v err=%v", done, err)
	}

	if answer.Summary != "S" || answer.Full != "F" || !answer.CostKnown || answer.CostUSD != 0.5 {
		t.Errorf("answer = %+v", answer)
	}

	failed := streamEvent{Type: "result", IsError: true, Result: "boom"}
	if _, done, err := handleEvent(failed, progress); !done || err == nil {
		t.Errorf("error result: done=%v err=%v", done, err)
	}
}

func TestChargeTurnUsesDeltas(t *testing.T) {
	t.Parallel()

	a := New("claude", nil)

	first := agent.Answer{CostUSD: 0.010, CostKnown: true}
	a.chargeTurn(&first)

	second := agent.Answer{CostUSD: 0.015, CostKnown: true}
	a.chargeTurn(&second)

	if first.CostUSD != 0.010 || second.CostUSD < 0.0049 || second.CostUSD > 0.0051 {
		t.Errorf("costs = %v, %v; want 0.010, 0.005", first.CostUSD, second.CostUSD)
	}
}

func TestTailBuffer(t *testing.T) {
	t.Parallel()

	var tb tailBuffer

	tb.max = 4

	if _, err := tb.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}

	if got := tb.String(); got != "cdef" {
		t.Errorf("String() = %q, want %q", got, "cdef")
	}
}

func TestSetsPermissionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"none", []string{"--model", "opus"}, false},
		{"empty", nil, false},
		{"separate value", []string{"--permission-mode", "plan"}, true},
		{"inline value", []string{"--permission-mode=acceptEdits"}, true},
		{"inherited", []string{"--inherit-permission-mode", "auto"}, true},
		{"skip permissions", []string{"--dangerously-skip-permissions"}, true},
		{"value only looks like a flag", []string{"--model", "--permission-mode-ish"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := setsPermissionMode(tt.args); got != tt.want {
				t.Errorf("setsPermissionMode(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
