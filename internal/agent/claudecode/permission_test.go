package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

const writeTool = "Write"

func TestBuildPromptForPermission(t *testing.T) {
	t.Parallel()

	req := controlRequest{
		Subtype:     subtypeCanUseTool,
		ToolName:    writeTool,
		DisplayName: writeTool,
		Description: "notes.txt",
		Input:       json.RawMessage(`{"file_path":"/tmp/notes.txt","content":"hi"}`),
		Suggestions: json.RawMessage(`[{"type":"setMode","mode":"acceptEdits","destination":"session"}]`),
	}

	p := buildPrompt("req-1", req)

	if p.Kind != agent.PromptPermission || p.Title != writeTool || p.Detail != "/tmp/notes.txt" {
		t.Fatalf("prompt = %+v", p)
	}

	if len(p.Questions) != 1 || p.Questions[0].ID != permissionDecision {
		t.Fatalf("questions = %+v", p.Questions)
	}

	var ids []string
	for _, o := range p.Questions[0].Options {
		ids = append(ids, o.ID)
	}

	want := []string{agent.OptionAllow, agent.OptionAlways, agent.OptionDeny}
	if len(ids) != len(want) {
		t.Fatalf("options = %v, want %v", ids, want)
	}

	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("options = %v, want %v", ids, want)
		}
	}
}

func TestBuildPromptWithoutSuggestionsOmitsAlways(t *testing.T) {
	t.Parallel()

	req := controlRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"ls"}`)}

	for _, o := range buildPrompt("req-1", req).Questions[0].Options {
		if o.ID == agent.OptionAlways {
			t.Fatal("offered always-allow with no suggestion to back it")
		}
	}
}

const askInput = `{"questions":[{"question":"Tabs or spaces?","header":"Indent","multiSelect":false,` +
	`"options":[{"label":"Spaces","description":"the common default"},{"label":"Tabs","description":"the Go standard"}]}]}`

func TestBuildPromptForQuestion(t *testing.T) {
	t.Parallel()

	req := controlRequest{ToolName: askQuestionTool, Input: json.RawMessage(askInput)}

	p := buildPrompt("req-2", req)
	if p.Kind != agent.PromptChoice || len(p.Questions) != 1 {
		t.Fatalf("prompt = %+v", p)
	}

	q := p.Questions[0]
	if q.ID != "q0" || q.Text != "Tabs or spaces?" || q.Label != "Indent" || q.Multi {
		t.Fatalf("question = %+v", q)
	}

	if len(q.Options) != 2 || q.Options[0].ID != "Spaces" || q.Options[1].Detail != "the Go standard" {
		t.Fatalf("options = %+v", q.Options)
	}
}

func TestDecidePermission(t *testing.T) {
	t.Parallel()

	req := controlRequest{
		ToolName:    writeTool,
		Input:       json.RawMessage(`{"file_path":"/tmp/x"}`),
		Suggestions: json.RawMessage(`[{"type":"setMode","mode":"acceptEdits","destination":"session"}]`),
	}

	tests := []struct {
		name           string
		choice         string
		wantBehavior   string
		wantPermission bool
	}{
		{"allow once", agent.OptionAllow, behaviorAllow, false},
		{"allow always carries the suggestions", agent.OptionAlways, behaviorAllow, true},
		{"deny", agent.OptionDeny, behaviorDeny, false},
		{"no answer denies", "", behaviorDeny, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reply := agent.Reply{Choices: map[string][]string{permissionDecision: {tt.choice}}}

			got := decide(req, reply)
			if got.Behavior != tt.wantBehavior {
				t.Errorf("behavior = %v, want %v", got.Behavior, tt.wantBehavior)
			}

			if got.UpdatedPermissions != nil != tt.wantPermission {
				t.Errorf("updatedPermissions = %s, want present = %v", got.UpdatedPermissions, tt.wantPermission)
			}
		})
	}
}

func TestDecideQuestionWritesAnswersIntoInput(t *testing.T) {
	t.Parallel()

	req := controlRequest{ToolName: askQuestionTool, Input: json.RawMessage(askInput)}
	reply := agent.Reply{Choices: map[string][]string{"q0": {"Tabs"}}}

	got := decide(req, reply)
	if got.Behavior != behaviorAllow {
		t.Fatalf("behavior = %v, want allow", got.Behavior)
	}

	input, ok := got.UpdatedInput.(map[string]any)
	if !ok {
		t.Fatalf("updatedInput = %T", got.UpdatedInput)
	}

	answers, ok := input["answers"].(map[string]string)
	if !ok || answers["Tabs or spaces?"] != "Tabs" {
		t.Fatalf("answers = %#v", input["answers"])
	}
}

func TestDecideQuestionJoinsMultipleChoices(t *testing.T) {
	t.Parallel()

	req := controlRequest{ToolName: askQuestionTool, Input: json.RawMessage(askInput)}
	reply := agent.Reply{Choices: map[string][]string{"q0": {"Tabs", "Spaces"}}}

	input, _ := decide(req, reply).UpdatedInput.(map[string]any)
	answers, _ := input["answers"].(map[string]string)

	if answers["Tabs or spaces?"] != "Tabs, Spaces" {
		t.Errorf("answers = %#v", answers)
	}
}

func TestDecideUnansweredQuestionDenies(t *testing.T) {
	t.Parallel()

	req := controlRequest{ToolName: askQuestionTool, Input: json.RawMessage(askInput)}

	if got := decide(req, agent.Reply{}); got.Behavior != behaviorDeny {
		t.Errorf("behavior = %v, want deny", got.Behavior)
	}
}
