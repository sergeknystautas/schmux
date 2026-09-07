package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func nudgeFixture(t *testing.T, name string) [][]byte {
	t.Helper()
	f, err := os.Open("../../assets/dashboard/src/lib/chat/__fixtures__/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines [][]byte
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 16*1024*1024)
	for s.Scan() {
		lines = append(lines, append([]byte(nil), s.Bytes()...))
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func assertNudge(t *testing.T, tr *NudgeTracker, state, summary string) {
	t.Helper()
	got := tr.Result()
	if got.State != state || got.Summary != summary {
		t.Errorf("want %s %q; got %s %q", state, summary, got.State, got.Summary)
	}
	if got.Source != "headless" {
		t.Errorf("source = %q, want headless", got.Source)
	}
}

func TestNudgeRegressionCapturedCodexQuestion(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	for _, line := range nudgeFixture(t, "codex/userinput.out.jsonl") {
		tr.Rec(NewHarness(line))
		var v struct {
			Method string `json:"method"`
		}
		json.Unmarshal(line, &v)
		if v.Method == "item/tool/requestUserInput" {
			assertNudge(t, tr, "Needs Input", "Which fruit?")
			return
		}
	}
	t.Fatal("fixture request not found")
}

func TestNudgeRegressionClaudeQueue(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewUserMessage("first", nil))
	tr.Rec(NewHarness([]byte(`{"type":"assistant","message":{"content":[]}}`)))
	tr.Rec(NewUserMessage("second", nil))
	for _, line := range nudgeFixture(t, "claude/queued.jsonl") {
		var v struct {
			Type string `json:"type"`
		}
		json.Unmarshal(line, &v)
		if v.Type == "result" {
			tr.Rec(NewHarness(line))
			assertNudge(t, tr, "Working", "")
			return
		}
	}
	t.Fatal("fixture result not found")
}

func TestNudgeRegressionInterruptIntent(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewUserMessage("hi", nil))
	tr.WroteInterrupt()
	assertNudge(t, tr, "Working", "")
}

func TestNudgeRegressionCapturedInterruptResult(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewUserMessage("hi", nil))
	tr.WroteInterrupt()
	for _, line := range nudgeFixture(t, "claude/interrupt-text.jsonl") {
		tr.Rec(NewHarness(line))
	}
	assertNudge(t, tr, "Idle", "")
}

func TestNudgeRegressionCancellation(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	for _, line := range nudgeFixture(t, "claude/interrupt-pending.jsonl") {
		tr.Rec(NewHarness(line))
		var v struct {
			Type string `json:"type"`
		}
		json.Unmarshal(line, &v)
		if v.Type == "control_cancel_request" {
			assertNudge(t, tr, "Working", "")
			return
		}
	}
	t.Fatal("fixture cancellation not found")
}

func TestNudgeRegressionCodexZeroAnswer(t *testing.T) {
	p := PathsFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("review", newCodexProtocol(), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	for _, line := range nudgeFixture(t, "codex/approval.out.jsonl") {
		var v struct {
			Method string `json:"method"`
		}
		json.Unmarshal(line, &v)
		writeOutput(t, p, string(line))
		if v.Method == "item/commandExecution/requestApproval" {
			break
		}
	}
	rt.drain() // deterministic input/output boundary, with no tail goroutine
	if err := rt.AnswerPermission("0", true, nil, ""); err != nil {
		t.Fatal(err)
	}
	assertNudge(t, rt.nudgeTracker, "Working", "")
	rt.Stop()
	restored, err := NewRuntime("review", newCodexProtocol(), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Stop)
	restored.Start()
	assertNudge(t, restored.nudgeTracker, "Working", "")
}

func TestNudgeRegressionFailedInterruptReplay(t *testing.T) {
	rt, p := newRuntimeWithNudge(t, nil)
	rt.Start()
	if _, err := rt.Send("hi", nil); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.appendInput = func([]byte) error { return errors.New("review write failure") }
	rt.mu.Unlock()
	if err := rt.Interrupt(); err == nil {
		t.Fatal("expected write failure")
	}
	assertNudge(t, rt.nudgeTracker, "Working", "")
	rt.Stop()
	restored, err := NewRuntime("review", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Stop)
	restored.Start()
	assertNudge(t, restored.nudgeTracker, "Working", "")
}

func TestNudgeRegressionMultipleQuestions(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewHarness([]byte(`{"type":"control_request","request_id":"q","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","requires_user_interaction":true,"input":{"questions":[{"question":"Which fruit?"},{"question":"Which color?"}]}}}`)))
	assertNudge(t, tr, "Needs Input", "Which fruit? (+1 more)")
}

func TestNudgeRegressionCodexUnrecoverableError(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"turn":{"id":"t"}}}`)))
	tr.Rec(NewHarness([]byte(`{"method":"error","params":{"error":{"message":"gone"},"willRetry":false}}`)))
	assertNudge(t, tr, "Error", "gone")
}

func TestNudgeRegressionClaudeErrorStrings(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["API unavailable"]}`)))
	assertNudge(t, tr, "Error", "API unavailable")
}

func TestNudgeRegressionUnsupportedRequest(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"turn":{"id":"t"}}}`)))
	tr.Rec(NewHarness([]byte(`{"method":"item/newTool/requestApproval","id":9,"params":{"threadId":"th","turnId":"t"}}`)))
	if got := tr.Result(); got.State != "Needs Input" {
		t.Errorf("want Needs Input for abort-only request; got %s %q", got.State, got.Summary)
	}
}

func TestNudgeRegressionCodexChildStarted(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"threadId":"parent-thread","turn":{"id":"parent"}}}`)))
	tr.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"threadId":"child-thread","turn":{"id":"child"}}}`)))
	tr.Rec(NewHarness([]byte(`{"method":"turn/completed","params":{"threadId":"child-thread","turn":{"id":"child","status":"completed"}}}`)))
	assertNudge(t, tr, "Working", "")
}

func TestNudgeRegressionReplyDoesNotResolveServerRequest(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"turn":{"id":"t"}}}`)))
	tr.Rec(NewHarness([]byte(`{"method":"item/commandExecution/requestApproval","id":5,"params":{"kind":"command","command":"ls"}}`)))
	tr.Rec(NewHarness([]byte(`{"id":5,"result":{"turn":{"id":"t","status":"inProgress"}}}`)))
	assertNudge(t, tr, "Needs Input", "Approve command: ls")
}

func TestNudgeRegressionLateClaudeChild(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(NewUserMessage("hi", nil))
	tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"success"}`)))
	for _, line := range nudgeFixture(t, "claude/subagent.jsonl") {
		var v struct {
			Type   string `json:"type"`
			Parent string `json:"parent_tool_use_id"`
		}
		json.Unmarshal(line, &v)
		if v.Type == "assistant" && v.Parent != "" {
			tr.Rec(NewHarness(line))
			assertNudge(t, tr, "Completed", "Done")
			return
		}
	}
	t.Fatal("child assistant not found")
}

func TestNudgeClaudeQueuedCapturedTurns(t *testing.T) {
	for _, fixture := range []string{"queued.jsonl", "queued-interrupt.jsonl"} {
		t.Run(fixture, func(t *testing.T) {
			lines := nudgeFixture(t, "claude/"+fixture)
			var messages []string
			for _, line := range lines {
				var v struct {
					IsReplay bool `json:"isReplay"`
					Message  struct {
						Content json.RawMessage `json:"content"`
					} `json:"message"`
				}
				if err := json.Unmarshal(line, &v); err != nil {
					t.Fatal(err)
				}
				if v.IsReplay {
					messages = append(messages, claudeMessageText(v.Message.Content))
				}
			}
			if len(messages) != 2 {
				t.Fatalf("fixture messages: %v", messages)
			}
			tr := NewNudgeTracker(ProtocolClaude)
			tr.Rec(NewUserMessage(messages[0], nil))
			queued, results := false, 0
			for _, line := range lines {
				var v struct {
					Type     string `json:"type"`
					IsReplay bool   `json:"isReplay"`
				}
				if err := json.Unmarshal(line, &v); err != nil {
					t.Fatal(err)
				}
				if v.Type == "result" && results == 0 && fixture == "queued-interrupt.jsonl" {
					tr.WroteInterrupt()
				}
				tr.Rec(NewHarness(line))
				if v.IsReplay && !queued {
					tr.Rec(NewUserMessage(messages[1], nil))
					queued = true
				}
				if v.Type == "result" {
					results++
					if results == 1 {
						assertNudge(t, tr, "Working", "")
					}
				}
			}
			if results != 2 {
				t.Fatalf("results = %d", results)
			}
			assertNudge(t, tr, "Completed", "Done")
		})
	}
}

func TestNudgeClaudeIdenticalQueuedMessages(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	for range 3 {
		tr.Rec(NewUserMessage("same", nil))
	}
	for i := range 3 {
		tr.Rec(NewHarness([]byte(`{"type":"user","isReplay":true,"message":{"content":"same"}}`)))
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"success"}`)))
		if i < 2 {
			assertNudge(t, tr, "Working", "")
		}
	}
	assertNudge(t, tr, "Completed", "Done")
}

func TestNudgePendingInterruptAndResolution(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	for _, id := range []string{"a", "b"} {
		tr.Rec(NewHarness(asLine(map[string]any{"type": "control_request", "request_id": id,
			"request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": id}}})))
	}
	tr.WroteInterrupt()
	assertNudge(t, tr, "Needs Input", "Approve Bash: a (+1 more)")
	tr.Rec(NewHarness([]byte(`{"type":"control_cancel_request","request_id":"a"}`)))
	assertNudge(t, tr, "Needs Input", "Approve Bash: b")
	tr.Rec(NewHarness([]byte(`{"type":"result","is_error":true,"subtype":"error_during_execution"}`)))
	assertNudge(t, tr, "Idle", "")
}

func TestNudgeNonterminalEventsKeepState(t *testing.T) {
	for _, protocol := range []string{ProtocolClaude, ProtocolCodex} {
		t.Run(protocol, func(t *testing.T) {
			tr := NewNudgeTracker(protocol)
			tr.Rec(NewUserMessage("hi", nil))
			var lines []string
			if protocol == ProtocolCodex {
				lines = []string{
					`{"method":"turn/started","params":{"turn":{"id":"t"}}}`,
					`{"method":"error","params":{"error":{"message":"retrying"},"willRetry":true}}`,
					`{"method":"thread/status/changed","params":{"status":{"type":"idle"}}}`,
					`{"method":"turn/completed","params":{"turn":{"id":"t","status":"inProgress"}}}`,
				}
			} else {
				lines = []string{
					`{"type":"user","message":{"content":[{"type":"tool_result","is_error":true}]}}`,
					`{"type":"system","subtype":"status","status":"compacting"}`,
					`{"type":"stream_event","event":{"type":"content_block_delta"}}`,
				}
			}
			for _, line := range lines {
				tr.Rec(NewHarness([]byte(line)))
				assertNudge(t, tr, "Working", "")
			}
		})
	}
}

func TestNudgeRuntimeReplayControlOccurrences(t *testing.T) {
	cap := &nudgeCapture{}
	rt, p := newRuntimeWithNudge(t, cap.callback)
	request := `{"type":"control_request","request_id":"reused","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`
	writeOutput(t, p, request)
	rt.drain()
	if err := rt.AnswerPermission("reused", true, nil, ""); err != nil {
		t.Fatal(err)
	}
	writeOutput(t, p, request)
	rt.drain()
	rt.appendInput = func([]byte) error { return errors.New("input full") }
	if err := rt.AnswerPermission("reused", true, nil, ""); err == nil {
		t.Fatal("expected input error")
	}
	assertNudge(t, rt.nudgeTracker, "Needs Input", "Approve Bash: ls")
	rt.Stop()
	restored, err := NewRuntime("restore", mustProto(t), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Stop)
	restoredCap := &nudgeCapture{}
	restored.SetNudgeCallback(restoredCap.callback)
	restored.Start()
	if got := restoredCap.all(); len(got) != 1 || got[0] != cap.last() {
		t.Fatalf("restored publications = %v; live = %v", got, cap.last())
	}
}

func TestNudgeCodexRejectedInterruptPreservesCompleted(t *testing.T) {
	rt, err := NewRuntime("codex", newCodexProtocol(), PathsFor(t.TempDir()), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	rt.nudgeTracker.Rec(NewHarness([]byte(`{"method":"turn/started","params":{"turn":{"id":"t"}}}`)))
	rt.nudgeTracker.Rec(NewHarness([]byte(`{"method":"turn/completed","params":{"turn":{"id":"t","status":"completed"}}}`)))
	if err := rt.Interrupt(); err == nil {
		t.Fatal("expected no-turn error")
	}
	assertNudge(t, rt.nudgeTracker, "Completed", "Done")
}

func TestNudgeCodexAbortZeroAndRestore(t *testing.T) {
	p := PathsFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime("codex", newCodexProtocol(), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Stop)
	writeOutput(t, p,
		`{"method":"turn/started","params":{"threadId":"root","turn":{"id":"t"}}}`,
		`{"id":0,"method":"item/unknown/requestApproval","params":{"threadId":"root","turnId":"t"}}`)
	rt.drain()
	assertNudge(t, rt.nudgeTracker, "Needs Input", "Respond to item/unknown/requestApproval")
	if err := rt.Abort("0"); err != nil {
		t.Fatal(err)
	}
	assertNudge(t, rt.nudgeTracker, "Working", "")
	rt.Stop()
	restored, err := NewRuntime("codex", newCodexProtocol(), p, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Stop)
	restored.Start()
	assertNudge(t, restored.nudgeTracker, "Working", "")
}
