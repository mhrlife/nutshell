//go:build integration

package codex

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

// No model turns are started: these tests check the installed CLI's protocol,
// effective approval configuration and recovery of threads with no rollout.
func TestInstalledCodex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		policy   string
		reviewer string
	}{
		{name: "auto review", policy: "on-request", reviewer: "auto_review"},
		{
			name: "manual override", args: []string{"-c", `approvals_reviewer="user"`},
			policy: "on-request", reviewer: "user",
		},
		{
			name: "policy override", args: []string{`--config=approval_policy="never"`},
			policy: "never", reviewer: "auto_review",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := installedAgent(t, tc.args)
			r := installedRun(t, a)

			raw, err := r.call(testContext(t), "thread/start", map[string]any{
				"ephemeral": true, "developerInstructions": "Reply with a summary and full answer.",
			})
			if err != nil {
				t.Fatal(err)
			}

			var response struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
				ApprovalPolicy    string `json:"approvalPolicy"`
				ApprovalsReviewer string `json:"approvalsReviewer"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}

			if response.Thread.ID == "" {
				t.Fatal("missing thread id")
			}

			if response.ApprovalPolicy != tc.policy || response.ApprovalsReviewer != tc.reviewer {
				t.Fatalf("effective approval settings: policy=%q reviewer=%q", response.ApprovalPolicy, response.ApprovalsReviewer)
			}
		})
	}
}

func TestInstalledCodexRecoversUnstartedThread(t *testing.T) {
	t.Parallel()
	a := installedAgent(t, nil)
	first := installedRun(t, a)

	var metadata agent.Event

	first.handler = agent.ProgressFunc(func(ev agent.Event) { metadata = ev })
	req := agent.Request{}

	oldID, err := a.openThread(testContext(t), req, first)
	if err != nil {
		t.Fatal(err)
	}

	if metadata.Kind != agent.KindMetadata || metadata.Model == "" || metadata.Cwd == "" {
		t.Fatalf("thread metadata = %+v", metadata)
	}

	first.close()
	first.proc.close()

	second := installedRun(t, a)

	newID, err := a.openThread(testContext(t), req, second)
	if err != nil {
		t.Fatal(err)
	}

	if newID == oldID {
		t.Fatal("expected a new thread after the unstarted thread was lost")
	}
}

func installedAgent(t *testing.T, args []string) *Agent {
	t.Helper()

	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed")
	}

	return New(bin, args, slog.New(slog.DiscardHandler))
}

func installedRun(t *testing.T, a *Agent) *run {
	t.Helper()

	p, err := startProcess(t.Context(), a.argv)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(p.close)
	r := newRun(p, nil, a.logger)
	t.Cleanup(r.close)

	_, err = r.call(testContext(t), "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "nutshell", "version": "test"},
		"capabilities": map[string]bool{"experimentalApi": true},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.send(map[string]string{keyMethod: "initialized"}); err != nil {
		t.Fatal(err)
	}

	return r
}
