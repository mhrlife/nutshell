package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/mhrlife/nutshell/internal/agent"
)

const (
	testAnswer     = "answer"
	testResumeRoot = "thread/resume:root"
	testMissing    = "missing"
	testSide       = "side"
	testWithdraw   = "withdraw"
)

// TestCodexProcess is a protocol peer, launched as a child test executable.
// It checks requests on the wire and never invokes Codex or a model.
func TestCodexProcess(t *testing.T) {
	t.Parallel()

	if !slices.Contains(os.Args, "nutshell-codex-helper") {
		return
	}

	if err := mockServer(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(0)
}

type mockPeer struct {
	enc                                *json.Encoder
	method, thread, instructions, text string
	initialized                        bool
}

func (p *mockPeer) send(v any) { _ = p.enc.Encode(v) }

func (p *mockPeer) event(method string, params any) {
	p.send(map[string]any{keyMethod: method, keyParams: params})
}

func mockServer() error {
	p := &mockPeer{enc: json.NewEncoder(os.Stdout)}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 65536), 1<<20)

	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return err
		}

		if err := p.handle(m); err != nil {
			return err
		}
	}

	return sc.Err()
}

type mockParams struct {
	ThreadID              string `json:"threadId"`
	DeveloperInstructions string `json:"developerInstructions"`
	Input                 []struct {
		Text string `json:"text"`
	} `json:"input"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

func (p *mockPeer) handle(m message) error {
	var params mockParams
	if len(m.Params) > 0 {
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return err
		}
	}

	result := any(map[string]any{})

	switch m.Method {
	case "initialize":
		p.initialized = true
	case "initialized":
		return nil
	case "thread/start", resumeThread, "thread/fork":
		if !p.initialized {
			return errors.New("thread before initialize")
		}

		p.method, p.thread, p.instructions = m.Method, params.ThreadID, params.DeveloperInstructions
		if m.Method == "thread/start" {
			p.thread = agent.RootThread
		}

		if m.Method == "thread/fork" {
			p.thread = testSide
		}

		if p.rejectResume(m, params.ThreadID) {
			return nil
		}

		result = map[string]any{"thread": map[string]string{"id": p.thread}}
	case "turn/start":
		return p.begin(m, params)
	case "turn/interrupt":
		p.event("turn/completed", map[string]any{keyThreadID: p.thread, keyTurn: map[string]string{"id": keyTurn, "status": "interrupted"}})
	case "":
		return p.reply(m)
	default:
		return fmt.Errorf("unexpected method %s", m.Method)
	}

	p.send(map[string]any{"id": m.ID, "result": result})

	return nil
}

func (p *mockPeer) begin(m message, params mockParams) error {
	if params.ThreadID != p.thread || len(params.Input) != 1 || len(params.OutputSchema) == 0 {
		return errors.New("invalid turn params")
	}

	p.text = params.Input[0].Text
	p.event("turn/started", map[string]any{keyThreadID: p.thread, keyTurn: map[string]string{"id": keyTurn}})

	if p.text == "rpc-error" {
		p.send(map[string]any{"id": m.ID, keyError: rpcError{Code: -1, Message: "refused"}})
		return nil
	}

	if err := p.startTurn(); err != nil {
		return err
	}

	p.send(map[string]any{"id": m.ID, "result": map[string]any{keyTurn: map[string]string{"id": keyTurn}}})

	return nil
}

func (p *mockPeer) startTurn() error {
	if p.text == "exit" {
		return errors.New("simulated crash")
	}

	if p.text == "bad-json" {
		fmt.Fprintln(os.Stdout, "{broken")
		return nil
	}

	p.event("item/started", map[string]any{keyThreadID: p.thread, keyTurnID: keyTurn, "item": map[string]string{"id": "cmd", keyType: "commandExecution", "command": "go test ./..."}})

	if p.text == "wait" {
		return nil
	}

	if p.text == "prompts" || p.text == testWithdraw {
		p.send(map[string]any{"id": 100, keyMethod: commandApproval, keyParams: map[string]string{keyThreadID: p.thread, keyTurnID: keyTurn, "command": "go test", "itemId": "cmd"}})

		if p.text == testWithdraw {
			p.event("serverRequest/resolved", map[string]any{keyThreadID: p.thread, "requestId": 100})
			p.finish()
		}

		return nil
	}

	p.finish()

	return nil
}

func (p *mockPeer) reply(m message) error {
	if p.text == testWithdraw {
		return nil
	}

	if string(m.ID) == "100" {
		var result map[string]string
		if json.Unmarshal(m.Result, &result) != nil || result[decisionKey] != accept {
			return fmt.Errorf("bad approval: %s", m.Result)
		}

		p.event("serverRequest/resolved", map[string]any{keyThreadID: p.thread, "requestId": 100})
		p.send(map[string]any{"id": "question", keyMethod: userInput, keyParams: map[string]any{
			keyThreadID: p.thread, keyTurnID: keyTurn, "questions": []map[string]any{{"id": "q", "header": "Choice", "question": "Which?", "isOther": true}},
		}})

		return nil
	}

	var result struct {
		Answers map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	if json.Unmarshal(m.Result, &result) != nil || !slices.Equal(result.Answers["q"].Answers, []string{"custom answer"}) {
		return fmt.Errorf("bad user input: %s", m.Result)
	}

	p.finish()

	return nil
}

func (p *mockPeer) finish() {
	p.event("item/completed", map[string]any{keyThreadID: p.thread, keyTurnID: keyTurn, "item": map[string]string{keyType: "agentMessage", "phase": "commentary", keyText: "Working."}})

	answer, _ := json.Marshal(map[string]string{"summary": p.method + ":" + p.thread, "full": p.instructions + "\n" + p.text})
	if p.text == "invalid" {
		answer = []byte("not json")
	}

	if p.text != "empty" {
		p.event("item/completed", map[string]any{keyThreadID: p.thread, keyTurnID: keyTurn, "item": map[string]string{keyType: "agentMessage", "phase": "final_answer", keyText: string(answer)}})
	}

	status := "completed"
	if p.text == "failed" {
		status = "failed"
	}

	p.event("turn/completed", map[string]any{keyThreadID: p.thread, keyTurn: map[string]string{"id": keyTurn, "status": status}})
}

func (p *mockPeer) rejectResume(m message, id string) bool {
	if id != testMissing && id != "unavailable" {
		return false
	}

	failure := rpcError{Code: -32600, Message: "no rollout found for thread id " + id}
	if id == "unavailable" {
		failure.Message = "permission denied reading rollout"
	}

	p.send(map[string]any{"id": m.ID, keyError: failure})

	return true
}
