package main

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/mhrlife/nutshell/internal/cli"
	"github.com/mhrlife/nutshell/internal/config"
)

func TestNewCodexAgent(t *testing.T) {
	t.Parallel()

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	opts, err := cli.Parse([]string{"--agent", "codex", "--agent-bin", bin, "-c", `model="example"`}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	opts.Apply(&cfg)

	a, err := newAgent(cfg.Agent, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close() //nolint:errcheck // no process is started in this factory test

	if a.Name() != "codex" || len(cfg.Agent.Args) != 2 {
		t.Fatalf("agent = %s, args = %v", a.Name(), cfg.Agent.Args)
	}
}
