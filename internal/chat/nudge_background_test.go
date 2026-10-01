package chat

import "testing"

// Lines captured from Claude stream-json in session bach-godot-002-66e1a86f
// and bach-godot-002-36c96ef4 (uuid and session_id trimmed).
const (
	claudeBgCommitGates = "Run commit-gates (vendoring + corpus + check + data-validate + test + test-editor + test-web + test-mac-packaged)"
	claudeBgOneTask     = `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"btsud47kt","task_type":"local_bash","description":"Run commit-gates (vendoring + corpus + check + data-validate + test + test-editor + test-web + test-mac-packaged)"}]}`
	claudeBgTwoTasks    = `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"bj6g2opks","task_type":"local_bash","description":"Gate 6: native packaged runtime (mac)"},{"task_id":"boes9us2y","task_type":"local_bash","description":"Wait for test-mac-packaged to complete"}]}`
	claudeBgNone        = `{"type":"system","subtype":"background_tasks_changed","tasks":[]}`
	claudeTurnInit      = `{"type":"system","subtype":"init","cwd":"/tmp/ws","session_id":"s"}`
	claudeBgResult      = `{"type":"result","subtype":"success","is_error":false,"result":"Commit-gates running in the background. Ending the turn; will resume on the completion notification."}`
)

// bgTurnEnded drives a Claude turn that starts one background task and then
// ends successfully, the sequence observed in bach-godot-002-66e1a86f.
func bgTurnEnded(t *testing.T) *NudgeTracker {
	t.Helper()
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("run the gates"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness([]byte(claudeBgOneTask)))
	assertNudge(t, tr, "Working", "")
	tr.Rec(recHarness([]byte(claudeBgResult)))
	return tr
}

func TestNudge_ClaudeBackgroundTaskAfterTurnIsBackground(t *testing.T) {
	tr := bgTurnEnded(t)
	assertNudge(t, tr, "Background", claudeBgCommitGates)
}

func TestNudge_ClaudeBackgroundListEmptiesBecomesCompleted(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recHarness([]byte(claudeBgNone)))
	assertNudge(t, tr, "Completed", "Done")
}

func TestNudge_ClaudeBackgroundTwoTasksCountsMore(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("run the gates"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness([]byte(claudeBgTwoTasks)))
	tr.Rec(recHarness([]byte(claudeBgResult)))
	assertNudge(t, tr, "Background", "Gate 6: native packaged runtime (mac) (+1 more)")
}

// Claude resumes on the task's completion notification by opening a new
// turn on its own; an open turn outranks Background.
func TestNudge_ClaudeNewTurnWhileBackgroundIsWorking(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	assertNudge(t, tr, "Working", "")
	tr.Rec(recHarness([]byte(claudeBgResult)))
	assertNudge(t, tr, "Background", claudeBgCommitGates)
}

// Review Focus 1: the user's follow-up opens a turn.
func TestNudge_ClaudeFollowUpWhileBackgroundIsWorking(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recUser("status?"))
	assertNudge(t, tr, "Working", "")
}

// Review Focus 4: an errored turn stays Error, before and after the
// background list empties.
func TestNudge_ClaudeErrorOutranksBackground(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("run the gates"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness([]byte(claudeBgOneTask)))
	tr.Rec(recHarness(asLine(map[string]any{
		"type":     "result",
		"subtype":  "error_during_execution",
		"is_error": true,
		"errors":   []string{"boom"},
	})))
	assertNudge(t, tr, "Error", "boom")
	tr.Rec(recHarness([]byte(claudeBgNone)))
	assertNudge(t, tr, "Error", "boom")
}

func TestNudge_ClaudePendingRequestOutranksBackground(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recHarness(asLine(map[string]any{
		"type":       "control_request",
		"request_id": "rq",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
		},
	})))
	assertNudge(t, tr, "Needs Input", "Approve Bash: ls")
}

func TestNudge_ClaudeInterruptWithBackgroundThenIdle(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("run the gates"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness([]byte(claudeBgOneTask)))
	tr.WroteInterrupt()
	tr.Rec(recHarness(asLine(map[string]any{
		"type":     "result",
		"subtype":  "error_during_execution",
		"is_error": true,
		"errors":   []string{"interrupted"},
	})))
	assertNudge(t, tr, "Background", claudeBgCommitGates)
	tr.Rec(recHarness([]byte(claudeBgNone)))
	assertNudge(t, tr, "Idle", "")
}

func TestNudge_ClaudeSessionEndedClearsBackground(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recSessionEnded())
	assertNudge(t, tr, "Idle", "")
}

// system/init opens every turn; background tasks survive it
// (bach-godot-002-36c96ef4: bk6jsbgd8 listed before and after an init).
func TestNudge_ClaudeInitKeepsBackground(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recHarness([]byte(claudeTurnInit)))
	assertNudge(t, tr, "Background", claudeBgCommitGates)
}

// Review Focus 2: a missing tasks key is an empty list.
func TestNudge_ClaudeBackgroundMissingTasksClears(t *testing.T) {
	tr := bgTurnEnded(t)
	tr.Rec(recHarness([]byte(`{"type":"system","subtype":"background_tasks_changed"}`)))
	assertNudge(t, tr, "Completed", "Done")
}

// Review Focus 3: never render an empty Background row.
func TestNudge_ClaudeBackgroundEmptyDescriptionFallsBack(t *testing.T) {
	tr := NewNudgeTracker(ProtocolClaude)
	tr.Rec(recUser("go"))
	tr.Rec(recHarness(asLine(map[string]any{"type": "assistant"})))
	tr.Rec(recHarness([]byte(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"x","task_type":"local_agent","description":"  "}]}`)))
	tr.Rec(recHarness([]byte(claudeBgResult)))
	assertNudge(t, tr, "Background", "Background task")
}
