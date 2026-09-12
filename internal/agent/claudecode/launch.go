package claudecode

import (
	"slices"
	"strings"

	"github.com/mhrlife/nutshell/internal/agent"
)

// streamJSON is the Claude Code stream format nutshell speaks in both directions.
const streamJSON = "stream-json"

// separator ends the arguments a wrapper host reads and hands the rest over.
const separator = "--"

// Flag names nutshell renders in more than one place.
const (
	flagInputFormat    = "--input-format"
	flagPermissionMode = "--permission-mode"
)

// Launch is how the Claude Code CLI is reached.
type Launch int

const (
	// DirectLaunch runs the Claude Code CLI itself, so nutshell passes every
	// flag the session needs.
	DirectLaunch Launch = iota
	// WrapperLaunch runs a host CLI that starts Claude Code on nutshell's
	// behalf. Such a host must render -p, --input-format, --output-format and
	// --verbose itself; must accept --resume ahead of a "--" separator, because
	// it resolves the session's working directory from that value; and must
	// forward everything after the separator to Claude Code unchanged.
	WrapperLaunch
)

// defaultPermissionMode is the mode nutshell asks for when the user has not
// picked one. Outside a terminal claude falls back to "default", which stops
// for every write and every bash command it does not consider harmless;
// interactive sessions get "auto" and its classifier, and so should we.
const defaultPermissionMode = "auto"

// permissionModeFlags are the agent flags that decide the permission mode.
var permissionModeFlags = []string{
	flagPermissionMode,
	"--inherit-permission-mode",
	"--dangerously-skip-permissions",
}

// setsPermissionMode reports whether the user's own flags already choose a
// permission mode, in which case nutshell leaves that choice alone.
func setsPermissionMode(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if slices.Contains(permissionModeFlags, name) {
			return true
		}
	}

	return false
}

// command returns the argv of one session: the host command, the arguments that
// host reads itself, then the flags Claude Code reads.
func (a *Agent) command() []string {
	argv := make([]string, 0, len(a.argv))
	argv = append(argv, a.argv...)
	argv = append(argv, a.hostArgs()...)

	return append(argv, a.engineArgs()...)
}

// hostArgs are read by a wrapper host rather than by Claude Code.
func (a *Agent) hostArgs() []string {
	if a.launch != WrapperLaunch {
		return nil
	}

	args := []string{flagInputFormat, streamJSON}
	if a.sessionID != "" {
		args = append(args, "--resume", a.sessionID)
	}

	return append(args, separator)
}

// engineArgs are the flags Claude Code itself reads.
func (a *Agent) engineArgs() []string {
	args := a.printArgs()
	args = append(args,
		"--append-system-prompt", agent.Instructions(a.language),
		// Route permission requests and questions to us over stdio instead
		// of letting claude deny them for want of anyone to ask.
		"--permission-prompt-tool", "stdio",
	)

	if !setsPermissionMode(a.extraArgs) {
		args = append(args, flagPermissionMode, defaultPermissionMode)
	}

	if a.launch == DirectLaunch && a.sessionID != "" {
		args = append(args, "--resume", a.sessionID)
	}

	return append(args, a.extraArgs...)
}

// printArgs selects print mode and the stream formats, which a wrapper host
// renders itself.
func (a *Agent) printArgs() []string {
	if a.launch == WrapperLaunch {
		return nil
	}

	return []string{
		"-p",
		flagInputFormat, streamJSON,
		"--output-format", streamJSON,
		"--verbose",
	}
}
