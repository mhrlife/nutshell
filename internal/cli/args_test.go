package cli

import (
	"io"
	"reflect"
	"testing"
)

const modelFlag = "--model"

func TestSplitArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantOurs   []string
		wantTheirs []string
	}{
		{
			name:       "mixed flags",
			args:       []string{"--port", "8080", modelFlag, "sonnet", "--no-open", "--mcp-config", "x.json"},
			wantOurs:   []string{"--port", "8080", "--no-open"},
			wantTheirs: []string{modelFlag, "sonnet", "--mcp-config", "x.json"},
		},
		{
			name:       "equals form",
			args:       []string{"--lang=fa", "--permission-mode=plan"},
			wantOurs:   []string{"--lang=fa"},
			wantTheirs: []string{"--permission-mode=plan"},
		},
		{
			name:       "single dash and positional",
			args:       []string{"-port", "1", "prompt text", "-c"},
			wantOurs:   []string{"-port", "1"},
			wantTheirs: []string{"prompt text", "-c"},
		},
		{
			name:       "nothing ours",
			args:       []string{"--verbose"},
			wantOurs:   nil,
			wantTheirs: []string{"--verbose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ours, theirs := splitArgs(tt.args)
			if !reflect.DeepEqual(ours, tt.wantOurs) {
				t.Errorf("ours = %v, want %v", ours, tt.wantOurs)
			}

			if !reflect.DeepEqual(theirs, tt.wantTheirs) {
				t.Errorf("theirs = %v, want %v", theirs, tt.wantTheirs)
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	getenv := func(k string) string {
		if k == "OPENROUTER_API_KEY" {
			return "env-key"
		}

		return ""
	}

	o, err := Parse([]string{"--lang", "fa", modelFlag, "opus"}, getenv, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if o.Lang != "fa" || o.APIKey != "env-key" || o.Agent != "claude" {
		t.Errorf("unexpected options: %+v", o)
	}

	if !reflect.DeepEqual(o.AgentArgs, []string{modelFlag, "opus"}) {
		t.Errorf("AgentArgs = %v", o.AgentArgs)
	}

	if _, err := Parse([]string{"--port", "x"}, getenv, io.Discard); err == nil {
		t.Error("expected an error for a malformed port")
	}
}
