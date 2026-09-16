package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const shutdownTimeout = 3 * time.Second

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("(%d): %s", e.Code, e.Message)
}

type received struct {
	message
	err error
}

// process owns a single stdio connection. The reader never writes to stdin;
// replies to user prompts and client requests share the serialized writer.
type process struct {
	stdin  io.WriteCloser
	mu     sync.Mutex
	events chan received
	stop   context.CancelFunc
	done   chan struct{}
	stderr tailBuffer
	err    error // published by closing events
}

func startProcess(ctx context.Context, argv []string) (*process, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the executable is chosen by the user
	cmd.WaitDelay = shutdownTimeout

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()

		_ = stdin.Close()

		return nil, fmt.Errorf("codex stdout: %w", err)
	}

	p := &process{stdin: stdin, events: make(chan received), stop: cancel, done: make(chan struct{})}

	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		cancel()

		_ = stdin.Close()
		_ = stdout.Close()

		return nil, fmt.Errorf("starting codex: %w", err)
	}

	go func() {
		defer close(p.done)
		defer close(p.events)

		err := p.read(ctx, stdout)

		cancel()

		waitErr := cmd.Wait()
		if err == nil {
			err = waitErr
		}

		if err == nil {
			err = io.EOF
		}

		p.err = fmt.Errorf("codex exited (%w): %s", err, p.stderr.String())
	}()

	return p, nil
}

func (p *process) read(ctx context.Context, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<20)

	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return fmt.Errorf("decoding codex message: %w", err)
		}

		select {
		case p.events <- received{message: m}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return sc.Err()
}

func (p *process) send(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := json.NewEncoder(p.stdin).Encode(v); err != nil {
		return fmt.Errorf("writing to codex: %w", err)
	}

	return nil
}

func (p *process) close() {
	p.stop()
	_ = p.stdin.Close()
	<-p.done
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, data...)

	const limit = 4096
	if len(b.data) > limit {
		b.data = append([]byte(nil), b.data[len(b.data)-limit:]...)
	}

	return len(data), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return string(b.data)
}
