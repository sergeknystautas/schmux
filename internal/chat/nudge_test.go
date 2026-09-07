package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

// asLine wraps a JSON object as the kind of payload a harness or control
// line arrives in: a single object. Test fixtures build the bytes
// directly; this helper just makes the call sites less noisy.
func asLine(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// recHarness builds a Record that wraps one harness-emitted line.
func recHarness(line []byte) Record {
	return Record{Type: RecordHarness, Line: json.RawMessage(line)}
}

// recControl wraps a control line as a control record.
func recControl(line []byte) Record {
	return Record{Type: RecordControl, Line: json.RawMessage(line)}
}

// recUser is a user_message record carrying the given text.
func recUser(text string) Record {
	return Record{Type: RecordUserMessage, ID: "msg-1", Text: text}
}

// recSessionEnded is a dispose-marker record.
func recSessionEnded() Record {
	return Record{Type: RecordSession, Event: "ended"}
}

func TestNudge_InitialIsIdle(t *testing.T) {
	for _, protocol := range []string{ProtocolClaude, ProtocolCodex} {
		t.Run(protocol, func(t *testing.T) {
			tr := NewNudgeTracker(protocol)
			if got := tr.Result(); got != (Nudge{State: "Idle", Source: "headless"}) {
				t.Fatalf("initial state: %+v", got)
			}
		})
	}
}

func TestNudge_ClaudePermissionRequestNeedsInput(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "req-1",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "python3 -c 'print(41+1)'"},
		},
	})))
	want := Nudge{State: "Needs Input", Summary: "Approve Bash: python3 -c 'print(41+1)'", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ClaudeQuestionRequestUsesFirstQuestion(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "req-q",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "AskUserQuestion",
			"input": map[string]any{
				"questions": []map[string]any{{"question": "Which option do you prefer?"}},
			},
		},
	})))
	want := Nudge{State: "Needs Input", Summary: "Which option do you prefer?", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ClaudeApprovalForFileChangeFallsBack(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "req-fc",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Edit",
			"input":     map[string]any{"file_path": "/tmp/x.go"},
		},
	})))
	want := Nudge{State: "Needs Input", Summary: "Approve Edit: /tmp/x.go", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ClaudeResultSuccessBecomesCompleted(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("hi"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":    "result",
		"subtype": "success",
		"result":  "Done",
	})))
	want := Nudge{State: "Completed", Summary: "Done", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ClaudeResultErrorBecomesError(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("hi"))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":    "result",
		"subtype": "error",
		"errors":  []string{"rate limit hit"},
	})))
	want := Nudge{State: "Error", Summary: "rate limit hit", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ClaudeErrorResultFallsBackToSubtype(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":    "result",
		"subtype": "error_max_turns",
	})))
	want := Nudge{State: "Error", Summary: "claude error_max_turns", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_ControlResponseRemovesPending(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "req-1",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	if got := tr.Result(); got.State != "Needs Input" {
		t.Fatalf("expected Needs Input, got %+v", got)
	}
	tr.WroteControl("req-1")
	// After resolution, a control_response record arrives. It is a
	// no-op for the tracker because the pending entry is already
	// removed, but the runtime still appends it; Rec it and confirm
	// the state is now Working (open turn continues).
	tr.Rec(recControl(asLine(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": "req-1",
			"response":   map[string]any{"behavior": "allow"},
		},
	})))
	want := Nudge{State: "Working", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_DuplicateRequestIdNotAddedTwice(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "dup",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "dup",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	if got := tr.Result(); strings.Contains(got.Summary, "(+") {
		t.Fatalf("expected single pending, got %+v", got)
	}
}

func TestNudge_TwoRequestsShowFirstAndCount(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "r1",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "r2",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	if got := tr.Result(); got.Summary != "Approve Bash: ls (+1 more)" {
		t.Fatalf("got %+v", got)
	}
}

func TestNudge_QueuedMessageDoesNotClearNeedsInput(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "rq",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	tr.Rec(recUser("follow-up"))
	want := Nudge{State: "Needs Input", Summary: "Approve Bash: ls", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_SessionEndedResetsToIdle(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("hi"))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":    "result",
		"subtype": "success",
	})))
	tr.Rec(recSessionEnded())
	if got := tr.Result(); got.State != "Idle" || got.Summary != "" {
		t.Fatalf("after session ended: %+v", got)
	}
}

func TestNudge_InterruptedIntentStaysWorking(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("hi"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.WroteInterrupt()
	// No result follows: intent must not close the turn.
	if got := tr.Result(); got.State != "Working" {
		t.Fatalf("got %+v", got)
	}
}

func TestNudge_CodexQuestionFlow(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recUser("ask me a question"))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "turn-1"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "item/tool/requestUserInput",
		"id":     0,
		"params": map[string]any{
			"threadId":   "th-1",
			"turnId":     "turn-1",
			"isBlocking": true,
			"questions": []map[string]any{{
				"question": "Which fruit?",
				"options":  []map[string]any{{"label": "Apple"}, {"label": "Banana"}},
			}},
		},
	})))
	want := Nudge{State: "Needs Input", Summary: "Which fruit?", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	// Server confirms the user answered.
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "serverRequest/resolved",
		"params": map[string]any{"requestId": 0},
	})))
	if got := tr.Result(); got.State != "Working" {
		t.Fatalf("after resolution got %+v", got)
	}
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "turn-1", "status": "completed"}},
	})))
	want = Nudge{State: "Completed", Summary: "Done", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("after completion got %+v want %+v", got, want)
	}
}

func TestNudge_CodexChildTurnDoesNotCloseParent(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "parent"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "item/tool/requestUserInput",
		"id":     0,
		"params": map[string]any{
			"threadId":   "th",
			"turnId":     "parent",
			"isBlocking": true,
			"questions":  []map[string]any{{"question": "q?"}},
		},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "child", "status": "completed"}},
	})))
	if got := tr.Result(); got.State != "Needs Input" {
		t.Fatalf("child turn closed parent: %+v", got)
	}
	// The parent's matching turn/completed closes the turn.
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "parent", "status": "completed"}},
	})))
	if got := tr.Result(); got.State != "Completed" {
		t.Fatalf("parent completion did not close: %+v", got)
	}
}

func TestNudge_CodexNonBlockingQuestionStillNeedsAnswer(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "turn-1"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "item/tool/requestUserInput",
		"id":     0,
		"params": map[string]any{
			"threadId":   "th-1",
			"turnId":     "turn-1",
			"isBlocking": false,
			"questions":  []map[string]any{{"question": "Are you sure?"}},
		},
	})))
	if got := tr.Result(); got.State != "Needs Input" {
		t.Fatalf("question still needs an answer, got %+v", got)
	}
}

func TestNudge_CodexFailedTurnBecomesError(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "t1"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{
			"turn": map[string]any{
				"id":     "t1",
				"status": "failed",
				"error":  map[string]any{"message": "rate limit"},
			},
		},
	})))
	want := Nudge{State: "Error", Summary: "rate limit", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_CodexApprovalRequestFromFixture(t *testing.T) {
	// Use the captured approval fixture.
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "t1"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "item/commandExecution/requestApproval",
		"id":     0,
		"params": map[string]any{
			"kind":    "command",
			"command": "/bin/zsh -lc \"python3 -c 'print(41+1)'\"",
		},
	})))
	want := Nudge{State: "Needs Input", Summary: "Approve command: /bin/zsh -lc \"python3 -c 'print(41+1)'\"", Source: "headless"}
	if got := tr.Result(); !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNudge_CodexApprovalResolveAndComplete(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "t1"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "item/commandExecution/requestApproval",
		"id":     0,
		"params": map[string]any{
			"kind":    "command",
			"command": "echo hi",
		},
	})))
	// User accepts via the websocket; runtime records the JSON-RPC
	// response (which Codex app-server consumes as the answer).
	tr.Rec(recControl(asLine(map[string]any{
		"id":     0,
		"result": map[string]any{"decision": "accept"},
	})))
	tr.WroteControl("0")
	if got := tr.Result(); got.State != "Working" {
		t.Fatalf("after resolve got %+v", got)
	}
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "t1", "status": "completed"}},
	})))
	if got := tr.Result(); got.State != "Completed" {
		t.Fatalf("after completion got %+v", got)
	}
}

func TestNudge_CodexInterruptedTurnBecomesIdle(t *testing.T) {
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "t1"}},
	})))
	tr.WroteInterrupt()
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "t1", "status": "interrupted"}},
	})))
	if got := tr.Result(); got.State != "Idle" {
		t.Fatalf("interrupted should be Idle, got %+v", got)
	}
}

func TestNudge_QueuedMessageBetweenTurnsStaysWorking(t *testing.T) {
	// Between turn A's completion and turn B's start, the user's
	// already-accepted message keeps the Nudge at Working, not
	// Completed.
	tr := NewNudgeTracker(ProtocolCodex)
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/started",
		"params": map[string]any{"turn": map[string]any{"id": "ta"}},
	})))
	tr.Rec(recHarness(asLine(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{"turn": map[string]any{"id": "ta", "status": "completed"}},
	})))
	// User accepted a new message before turn B started.
	tr.Rec(recUser("next please"))
	if got := tr.Result(); got.State != "Working" {
		t.Fatalf("queued between turns got %+v", got)
	}
}
