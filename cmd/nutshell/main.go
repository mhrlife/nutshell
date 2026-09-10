// Command nutshell wraps a coding agent (Claude Code today) in a voice-first
// web UI: speak a question, hear a short answer, open the full one on demand.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mhrlife/nutshell/internal/agent"
	"github.com/mhrlife/nutshell/internal/agent/claudecode"
	"github.com/mhrlife/nutshell/internal/browser"
	"github.com/mhrlife/nutshell/internal/cli"
	"github.com/mhrlife/nutshell/internal/server"
	"github.com/mhrlife/nutshell/internal/settings"
	"github.com/mhrlife/nutshell/internal/speech"
	"github.com/mhrlife/nutshell/internal/web"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 3 * time.Second
)

// Release builds set these with -ldflags -X (see .goreleaser.yml). The
// installers read the first field of --version to decide whether to reinstall,
// so the version must stay at the front of that line.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}

		slog.Error("nutshell", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(newLogger(false))

	opts, err := cli.Parse(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		return err
	}

	slog.SetDefault(newLogger(opts.Debug))

	if opts.Version {
		fmt.Fprintf(os.Stdout, "%s (%s, %s)\n", version, commit, date)

		return nil
	}

	ag, err := newAgent(opts)
	if err != nil {
		return err
	}
	defer ag.Close() //nolint:errcheck // best-effort cleanup at exit

	sp := speech.New(speech.Config{
		APIKey: opts.APIKey, STTModel: opts.STTModel, TTSModel: opts.TTSModel, Voice: opts.TTSVoice, Style: speech.ResolveStyle(opts.TTSPrompt),
	})
	if !sp.Enabled() {
		slog.Warn("no OpenRouter key (set OPENROUTER_API_KEY or --openrouter-key); voice is off, typing still works")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}

	settingsPath, err := settings.DefaultPath()
	if err != nil {
		return err
	}

	store := settings.New(settingsPath)
	handler := server.New(ag, sp, store, http.FS(web.FS()), server.Config{Lang: opts.Lang, Project: filepath.Base(cwd)})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("listening: %w", err)
	}

	url := "http://" + ln.Addr().String()
	httpSrv := &http.Server{Handler: handler, ReadHeaderTimeout: readHeaderTimeout}

	errCh := make(chan error, 1)

	go func() { errCh <- httpSrv.Serve(ln) }()

	slog.Info("ready", "url", url, "agent", ag.Name(), "dir", cwd, "settings", settingsPath)

	if len(opts.AgentArgs) > 0 {
		slog.Info("forwarding to agent", "args", strings.Join(opts.AgentArgs, " "))
	}

	if !opts.NoOpen {
		if err := browser.Open(ctx, url); err != nil {
			slog.Warn("could not open a browser", "error", err, "url", url)
		} else {
			slog.Info("opening browser", "url", url)
		}
	}

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return httpSrv.Shutdown(shutdownCtx)
}

// newLogger writes to stderr. With --debug it also carries the per-request,
// per-turn and per-OpenRouter-call detail that explains a UI which looks like
// it did nothing at all.
func newLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// newAgent picks the agent implementation named by the flags.
func newAgent(opts cli.Options) (agent.Agent, error) {
	switch opts.Agent {
	case "claude":
		bin := opts.AgentBin
		if bin == "" {
			bin = "claude"
		}

		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("agent executable: %w", err)
		}

		return claudecode.New(bin, opts.AgentArgs), nil
	default:
		return nil, fmt.Errorf("unknown agent %q (supported: claude)", opts.Agent)
	}
}
