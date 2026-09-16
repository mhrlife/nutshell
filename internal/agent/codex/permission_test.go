package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

func TestPermissionDecisions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		method    string
		choice    string
		decisions []json.RawMessage
		want      string
	}{
		{"allow command", commandApproval, agent.OptionAllow, nil, accept},
		{"deny command", commandApproval, agent.OptionDeny, nil, decline},
		{"dismiss command", commandApproval, "", nil, decline},
		{"no persistent grant", commandApproval, agent.OptionAlways, nil, decline},
		{"restricted decisions", commandApproval, agent.OptionAllow, []json.RawMessage{json.RawMessage(`"cancel"`)}, "cancel"},
		{"allow files", fileApproval, agent.OptionAllow, nil, accept},
		{"deny files", fileApproval, agent.OptionDeny, nil, decline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := approvalParams{AvailableDecisions: tc.decisions}
			reply := agent.Reply{Choices: map[string][]string{decisionKey: {tc.choice}}}

			raw, err := json.Marshal(promptResult(tc.method, params, reply))
			if err != nil {
				t.Fatal(err)
			}

			var result struct {
				Decision string `json:"decision"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}

			if result.Decision != tc.want {
				t.Fatalf("decision = %q, want %q", result.Decision, tc.want)
			}

			p, ok := buildPrompt(message{Method: tc.method}, params, "")
			if !ok {
				t.Fatal("approval not recognized")
			}

			for _, opt := range p.Questions[0].Options {
				if opt.ID == agent.OptionAlways {
					t.Fatal("offered a persistent permission")
				}
			}
		})
	}
}

func TestPermissionScope(t *testing.T) {
	t.Parallel()

	params := approvalParams{Permissions: json.RawMessage(`{"network":{"enabled":true}}`)}

	for _, allowed := range []bool{true, false} {
		name := "denied"
		if allowed {
			name = "allowed"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reply := agent.Reply{}
			if allowed {
				reply.Choices = map[string][]string{decisionKey: {agent.OptionAllow}}
			}

			raw, err := json.Marshal(promptResult(permissionsApproval, params, reply))
			if err != nil {
				t.Fatal(err)
			}

			var result struct {
				Scope       string          `json:"scope"`
				Permissions json.RawMessage `json:"permissions"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}

			want := `{}`
			if allowed {
				want = string(params.Permissions)
			}

			if result.Scope != keyTurn || string(result.Permissions) != want {
				t.Fatalf("grant = %s", raw)
			}
		})
	}
}

func TestPromptDetailsAndUnsupportedRequests(t *testing.T) {
	t.Parallel()

	p, ok := buildPrompt(message{Method: fileApproval}, approvalParams{Reason: "needs write access"}, "a.go\n+new content")
	if !ok || !strings.Contains(p.Detail, "a.go") || !strings.Contains(p.Detail, "+new content") {
		t.Fatalf("file approval = %+v", p)
	}

	for _, method := range []string{"mcpServer/elicitation/request", "item/tool/call"} {
		if _, ok := buildPrompt(message{Method: method}, approvalParams{}, ""); ok {
			t.Fatalf("accepted unsupported %s", method)
		}
	}

	secret := approvalParams{Questions: []inputQuestion{{ID: "password", IsSecret: true}}}
	if _, ok := buildPrompt(message{Method: userInput}, secret, ""); ok {
		t.Fatal("secret question would be exposed in chat")
	}
}
