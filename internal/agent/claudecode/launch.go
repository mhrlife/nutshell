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
	flagResume         = "--resume"
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

// hostArgs are read by a wrapper host rather than by Claude Code. The host
// resolves the working directory from --resume, so it is given the session
// being resumed even when that session belongs to another thread and this one
// is about to fork it: the fork flag alone is Claude Code's business.
func (a *Agent) hostArgs() []string {
	if a.launch != WrapperLaunch {
		return nil
	}

	args := []string{flagInputFormat, streamJSON}
	if id, _ := a.history(); id != "" {
		args = append(args, flagResume, id)
	}

	return append(args, separator)
}

// history says which conversation the next process is to carry on:
//
//   - a thread that has been asked something before resumes its own session;
//   - a side thread asked its first question resumes the session of the
//     thread it came from, and forks it, so it starts out knowing everything
//     that thread knows and nothing it says afterwards reaches it;
//   - anything else starts a conversation from nothing.
func (a *Agent) history() (id string, fork bool) {
	if own := a.sessions[a.thread.Name()]; own != "" {
		return own, false
	}

	if parent := a.sessions[a.thread.Parent]; a.thread.Parent != "" && parent != "" {
		return parent, true
	}

	return "", false
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

	id, fork := a.history()

	if a.launch == DirectLaunch && id != "" {
		args = append(args, flagResume, id)
	}

	// The wrapper host was handed the session to resume; this says what to do
	// with it, and is read by Claude Code in either launch.
	if fork {
		args = append(args, "--fork-session")
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
