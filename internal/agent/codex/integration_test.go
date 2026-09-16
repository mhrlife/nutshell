//go:build integration

package codex

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"testing"
)

// This checks the installed CLI's handshake and thread schema without
// starting a turn, invoking a model, or executing project commands.
func TestInstalledCodex(t *testing.T) {
	t.Parallel()

	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed")
	}

	p, err := startProcess(t.Context(), []string{bin, "app-server", "--listen", "stdio://"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	r := newRun(p, nil, slog.New(slog.DiscardHandler))
	defer r.close()

	ctx := testContext(t)

	_, err = r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "nutshell", "version": "test"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.send(map[string]string{keyMethod: "initialized"}); err != nil {
		t.Fatal(err)
	}

	raw, err := r.call(ctx, "thread/start", map[string]any{"ephemeral": true, "developerInstructions": "Reply with a summary and full answer."})
	if err != nil {
		t.Fatal(err)
	}

	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}

	if response.Thread.ID == "" {
		t.Fatal("missing thread id")
	}
}
