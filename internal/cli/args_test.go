package cli

import (
	"errors"
	"flag"
	"io"
	"reflect"
	"testing"

	"github.com/mhrlife/nutshell/internal/config"
)

const (
	modelFlag  = "--model"
	modelValue = "opus"
)

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

	o, err := Parse([]string{"--lang", "fa", modelFlag, modelValue, "--config", "/tmp/nutshell.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if o.ConfigPath != "/tmp/nutshell.json" {
		t.Errorf("ConfigPath = %q", o.ConfigPath)
	}

	if !reflect.DeepEqual(o.AgentArgs, []string{modelFlag, modelValue}) {
		t.Errorf("AgentArgs = %v", o.AgentArgs)
	}

	if _, err := Parse([]string{"--port", "x"}, io.Discard); err == nil {
		t.Error("expected an error for a malformed port")
	}
}

func TestParseConfigCommand(t *testing.T) {
	t.Parallel()

	o, err := Parse([]string{configCommand, "path"}, io.Discard)
	if err != nil || o.Command != CommandConfigPath {
		t.Errorf("config path: Command = %q, err = %v", o.Command, err)
	}

	if _, err := Parse([]string{configCommand}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("bare config: err = %v, want flag.ErrHelp", err)
	}

	if _, err := Parse([]string{configCommand, "edit"}, io.Discard); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Errorf("unknown config command: err = %v, want an error", err)
	}

	// Only the first word selects a subcommand; elsewhere "config" is the agent's.
	o, err = Parse([]string{"--lang", "fa", configCommand}, io.Discard)
	if err != nil || o.Command != "" || !reflect.DeepEqual(o.AgentArgs, []string{configCommand}) {
		t.Errorf("config after a flag: %+v, err = %v", o, err)
	}
}

// Only the flags that were given replace what the file says; the rest keep
// the file's value even where it differs from the built-in default.
func TestApply(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Lang = "fa"
	cfg.TTS.Voice = "Kore"
	cfg.OpenBrowser = false
	cfg.Agent.Args = []string{"--permission-mode", "plan"}

	o, err := Parse([]string{"--tts-model", "gpt-4o-mini-tts", "--debug", modelFlag, modelValue}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	o.Apply(&cfg)

	if cfg.TTS.Model != "gpt-4o-mini-tts" || !cfg.Debug {
		t.Errorf("given flags were not applied: %+v", cfg)
	}

	if cfg.Lang != "fa" || cfg.TTS.Voice != "Kore" || cfg.OpenBrowser {
		t.Errorf("flags that were not given replaced the file's values: %+v", cfg)
	}

	if want := []string{"--permission-mode", "plan", modelFlag, modelValue}; !reflect.DeepEqual(cfg.Agent.Args, want) {
		t.Errorf("agent args = %v, want %v", cfg.Agent.Args, want)
	}
}
