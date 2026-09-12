package claudecode

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

// claudeBin stands in for the Claude Code CLI's name on PATH.
const claudeBin = "claude"

// instructions is what --append-system-prompt carries for the zero language.
func instructions() string { return agent.Instructions(lang.Language{}) }

func TestCommandDirectLaunch(t *testing.T) {
	t.Parallel()

	a := New([]string{claudeBin}, DirectLaunch, []string{"--mcp-config", "mcp.json"}, slog.Default())

	want := []string{
		claudeBin,
		"-p",
		flagInputFormat, streamJSON,
		"--output-format", streamJSON,
		"--verbose",
		"--append-system-prompt", instructions(),
		"--permission-prompt-tool", "stdio",
		flagPermissionMode, defaultPermissionMode,
		"--mcp-config", "mcp.json",
	}

	if got := a.command(); !slices.Equal(got, want) {
		t.Errorf("command() = %v, want %v", got, want)
	}
}

// A wrapper host renders print mode and both stream formats itself, so nutshell
// must not repeat them, and its own flags must precede the separator.
func TestCommandWrapperLaunch(t *testing.T) {
	t.Parallel()

	a := New([]string{"divar-copilot", "agent"}, WrapperLaunch, nil, slog.Default())

	want := []string{
		"divar-copilot", "agent",
		flagInputFormat, streamJSON,
		"--",
		"--append-system-prompt", instructions(),
		"--permission-prompt-tool", "stdio",
		flagPermissionMode, defaultPermissionMode,
	}

	if got := a.command(); !slices.Equal(got, want) {
		t.Errorf("command() = %v, want %v", got, want)
	}
}

// A wrapper resolves the session's working directory from its own --resume, so
// the flag belongs with the host arguments rather than the Claude Code flags.
func TestCommandResumePlacement(t *testing.T) {
	t.Parallel()

	direct := New([]string{claudeBin}, DirectLaunch, nil, slog.Default())
	direct.sessionID = "sess-1"

	wrapped := New([]string{"divar-copilot", "agent"}, WrapperLaunch, nil, slog.Default())
	wrapped.sessionID = "sess-1"

	if got := direct.command(); indexOf(got, "--resume") < indexOf(got, "--append-system-prompt") {
		t.Errorf("direct launch put --resume before the Claude Code flags: %v", got)
	}

	got := wrapped.command()
	if r, sep := indexOf(got, "--resume"), indexOf(got, "--"); r < 0 || sep < 0 || r > sep {
		t.Errorf("wrapper launch must put --resume before the separator: %v", got)
	}

	if slices.Contains(got, "-p") || slices.Contains(got, "--output-format") {
		t.Errorf("wrapper launch must not render the host's own flags: %v", got)
	}
}

// A permission mode the user passed wins, in either launch.
func TestCommandKeepsUserPermissionMode(t *testing.T) {
	t.Parallel()

	for _, launch := range []Launch{DirectLaunch, WrapperLaunch} {
		a := New([]string{"host"}, launch, []string{flagPermissionMode + "=plan"}, slog.Default())

		if got := a.command(); slices.Contains(got, defaultPermissionMode) {
			t.Errorf("launch %d overrode the user's permission mode: %v", launch, got)
		}
	}
}

func indexOf(args []string, want string) int {
	return slices.Index(args, want)
}
