package claudecode

// Starting the claude executable: the flags one turn's conversation needs,
// and the plumbing that keeps the process talking to us over stdio.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/lang"
)

// process is one running claude executable.
type process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdinMu sync.Mutex // answers to control requests are written from many goroutines
	events  <-chan streamEvent
	stderr  *tailBuffer
	stop    context.CancelFunc
}

// send writes one newline-terminated JSON message to the process.
func (p *process) send(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}

	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()

	_, err = p.stdin.Write(append(line, '\n'))

	return err
}

// defaultPermissionMode is the mode nutshell asks for when the user has not
// picked one. Outside a terminal claude falls back to "default", which stops
// for every write and every bash command it does not consider harmless;
// interactive sessions get "auto" and its classifier, and so should we.
const defaultPermissionMode = "auto"

// permissionModeFlags are the agent flags that decide the permission mode.
var permissionModeFlags = []string{
	"--permission-mode",
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

// historyArgs says which conversation the process is to carry on, given the
// claude session recorded for each thread so far:
//
//   - a thread that has been asked something before resumes its own session;
//   - a side thread asked its first question resumes its parent's session and
//     forks it, so it starts out knowing everything the parent knows and
//     nothing it says afterwards reaches the parent;
//   - anything else starts a conversation from nothing.
func historyArgs(sessions map[string]string, t agent.Thread) []string {
	if id := sessions[t.Name()]; id != "" {
		return []string{resumeFlag, id}
	}

	if id := sessions[t.Parent]; t.Parent != "" && id != "" {
		return []string{resumeFlag, id, "--fork-session"}
	}

	return nil
}

// resumeFlag is how claude is told which conversation to carry on.
const resumeFlag = "--resume"

// start runs claude for thread t with the rules of language l. The caller
// holds a.mu.
func (a *Agent) startLocked(l lang.Language, t agent.Thread) (*process, error) {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--append-system-prompt", agent.Instructions(l),
		// Route permission requests and questions to us over stdio instead
		// of letting claude deny them for want of anyone to ask.
		"--permission-prompt-tool", "stdio",
	}

	if !setsPermissionMode(a.extraArgs) {
		args = append(args, "--permission-mode", defaultPermissionMode)
	}

	args = append(args, historyArgs(a.sessions, t)...)
	args = append(args, a.extraArgs...)

	ctx, stop := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, a.bin, args...) //nolint:gosec // running the user's own agent with the flags they passed

	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()

		return nil, fmt.Errorf("claude stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()

		return nil, fmt.Errorf("claude stdout: %w", err)
	}

	stderr := &tailBuffer{max: stderrTail}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		stop()

		return nil, fmt.Errorf("starting claude: %w", err)
	}

	events := make(chan streamEvent, eventChanSize)

	go readEvents(cmd, stdout, events)

	proc := &process{cmd: cmd, stdin: stdin, events: events, stderr: stderr, stop: stop}
	// Announce ourselves as the host that answers control requests. Claude
	// Code carries on without this, so a failure here is not fatal.
	_ = proc.send(map[string]any{
		"type":       "control_request",
		"request_id": "nutshell-initialize",
		"request":    map[string]any{"subtype": "initialize"},
	})

	return proc, nil
}

// readEvents decodes stdout line by line until the process exits, then closes events.
func readEvents(cmd *exec.Cmd, stdout io.Reader, events chan<- streamEvent) {
	defer close(events)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), maxLineBytes)

	for sc.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // claude occasionally prints non-JSON diagnostics
		}

		events <- ev
	}

	_ = cmd.Wait()
}

func userMessage(text string) map[string]any {
	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	}
}
