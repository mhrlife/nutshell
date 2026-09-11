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
	// The level is only known once the flags are parsed. A LevelVar lets the
	// logger exist before that, so even a bad flag is reported through it.
	var level slog.LevelVar

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &level}))

	if err := run(logger, &level); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}

		logger.Error("nutshell", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, level *slog.LevelVar) error {
	opts, err := cli.Parse(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		return err
	}

	if opts.Version {
		fmt.Fprintf(os.Stdout, "%s (%s, %s)\n", version, commit, date)

		return nil
	}

	if opts.Debug {
		// --debug adds the per-request, per-turn and per-OpenRouter-call
		// detail that explains a UI which looks like it did nothing at all.
		level.Set(slog.LevelDebug)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ag, err := newAgent(opts, logger)
	if err != nil {
		return err
	}
	defer ag.Close() //nolint:errcheck // best-effort cleanup at exit

	sp := speech.New(speech.Config{
		APIKey: opts.APIKey, STTModel: opts.STTModel, SummaryModel: opts.SummaryModel,
		TTSModel: opts.TTSModel, Voice: opts.TTSVoice, Style: speech.ResolveStyle(opts.TTSStyle),
		Speed: opts.TTSSpeed,
	}, logger)
	if !sp.Enabled() {
		logger.WarnContext(ctx, "no OpenRouter key (set OPENROUTER_API_KEY or --openrouter-key); voice is off, typing still works")
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
	cfg := server.Config{Lang: opts.Lang, Project: filepath.Base(cwd)}
	handler := server.New(ag, sp, store, http.FS(web.FS()), cfg, logger)

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("listening: %w", err)
	}

	url := "http://" + ln.Addr().String()
	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)

	go func() { errCh <- httpSrv.Serve(ln) }()

	logger.InfoContext(ctx, "ready", "url", url, "agent", ag.Name(), "dir", cwd, "settings", settingsPath)

	if len(opts.AgentArgs) > 0 {
		logger.InfoContext(ctx, "forwarding to agent", "args", strings.Join(opts.AgentArgs, " "))
	}

	if !opts.NoOpen {
		openBrowser(ctx, logger, url)
	}

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.InfoContext(ctx, "shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return httpSrv.Shutdown(shutdownCtx)
}

// openBrowser shows the UI. Failing to is only worth a warning: the URL is
// already in the log.
func openBrowser(ctx context.Context, logger *slog.Logger, url string) {
	if err := browser.Open(ctx, url); err != nil {
		logger.WarnContext(ctx, "could not open a browser", "error", err, "url", url)

		return
	}

	logger.InfoContext(ctx, "opening browser", "url", url)
}

// newAgent picks the agent implementation named by the flags.
func newAgent(opts cli.Options, logger *slog.Logger) (agent.Agent, error) {
	switch opts.Agent {
	case "claude":
		bin := opts.AgentBin
		if bin == "" {
			bin = "claude"
		}

		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("agent executable: %w", err)
		}

		return claudecode.New(bin, opts.AgentArgs, logger), nil
	default:
		return nil, fmt.Errorf("unknown agent %q (supported: claude)", opts.Agent)
	}
}
