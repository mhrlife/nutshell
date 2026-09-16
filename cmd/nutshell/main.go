// Command nutshell wraps a coding agent (Claude Code today) in a voice-first
// web UI: speak a question, hear a short answer, open the full one on demand.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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
	"github.com/mhrlife/nutshell/internal/agent/codex"
	"github.com/mhrlife/nutshell/internal/browser"
	"github.com/mhrlife/nutshell/internal/cli"
	"github.com/mhrlife/nutshell/internal/config"
	"github.com/mhrlife/nutshell/internal/server"
	"github.com/mhrlife/nutshell/internal/settings"
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
	opts, err := cli.Parse(os.Args[1:], os.Stderr)
	if err != nil {
		return err
	}

	if done, err := runCommand(opts); done {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, cfgPath, err := loadConfig(ctx, logger, opts)
	if err != nil {
		return err
	}

	if cfg.Debug {
		// Debug adds the per-request, per-turn and per-speech-call detail
		// that explains a UI which looks like it did nothing at all.
		level.Set(slog.LevelDebug)
	}

	ag, err := newAgent(cfg.Agent, logger)
	if err != nil {
		return err
	}
	defer ag.Close() //nolint:errcheck // best-effort cleanup at exit

	sp := newSpeech(ctx, logger, cfg, cfgPath)

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}

	settingsPath, err := settings.DefaultPath()
	if err != nil {
		return err
	}

	store := settings.New(settingsPath)
	srvCfg := server.Config{Lang: cfg.Lang, Project: filepath.Base(cwd), Cwd: cwd}
	handler := server.New(ag, sp, store, http.FS(web.FS()), srvCfg, logger)

	ln, err := listen(ctx, logger, cfg.Port)
	if err != nil {
		return err
	}

	url := "http://" + ln.Addr().String()
	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)

	go func() { errCh <- httpSrv.Serve(ln) }()

	logger.InfoContext(ctx, "ready", "url", url, "agent", ag.Name(), "dir", cwd, "config", cfgPath, "settings", settingsPath)

	if len(cfg.Agent.Args) > 0 {
		logger.InfoContext(ctx, "forwarding to agent", "args", strings.Join(cfg.Agent.Args, " "))
	}

	if cfg.OpenBrowser {
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

// runCommand runs what the command line asked for instead of the UI, and
// reports whether it did. Output goes to stdout alone, so a script can use it:
// $EDITOR "$(nutshell config path)".
func runCommand(opts cli.Options) (bool, error) {
	switch {
	case opts.Version:
		fmt.Fprintf(os.Stdout, "%s (%s, %s)\n", version, commit, date)

		return true, nil
	case opts.Command == cli.CommandConfigPath:
		path, err := config.DefaultPath()
		if err != nil {
			return true, err
		}

		fmt.Fprintln(os.Stdout, path)

		return true, nil
	default:
		return false, nil
	}
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

// The --agent values nutshell understands.
const (
	agentClaude        = "claude"
	agentClaudeWrapper = "claude-wrapper"
	agentCodex         = "codex"
)

// defaultClaudeBin is the Claude Code CLI's own name on PATH.
const defaultClaudeBin = "claude"

// newAgent picks the agent implementation the configuration names.
func newAgent(cfg config.Agent, logger *slog.Logger) (agent.Agent, error) {
	if cfg.Name == agentCodex {
		bin := cfg.Bin
		if bin == "" {
			bin = agentCodex
		}

		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("agent executable: %w", err)
		}

		return codex.New(bin, cfg.Args, logger), nil
	}

	launch, err := launchFor(cfg.Name)
	if err != nil {
		return nil, err
	}

	argv := agentCommand(cfg.Bin, launch)
	if len(argv) == 0 {
		return nil, fmt.Errorf(
			"agent %s needs agent.bin (or --agent-bin) naming the host command, for example "+
				`--agent-bin "divar-copilot agent"`, cfg.Name)
	}

	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("agent executable: %w", err)
	}

	return claudecode.New(argv, launch, cfg.Args, logger), nil
}

func launchFor(name string) (claudecode.Launch, error) {
	switch name {
	case agentClaude:
		return claudecode.DirectLaunch, nil
	case agentClaudeWrapper:
		return claudecode.WrapperLaunch, nil
	default:
		return 0, fmt.Errorf("unknown agent %q (supported: %s, %s, %s)",
			name, agentClaude, agentClaudeWrapper, agentCodex)
	}
}

// agentCommand turns agent.bin into the argv that starts the agent. A wrapper
// host is named together with its subcommand, so its value is split on spaces;
// a direct launch keeps the value whole, because it is one executable path.
func agentCommand(bin string, launch claudecode.Launch) []string {
	if launch == claudecode.WrapperLaunch {
		return strings.Fields(bin)
	}

	if bin == "" {
		return []string{defaultClaudeBin}
	}

	return []string{bin}
}
