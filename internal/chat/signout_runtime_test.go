package chat

import "testing"

// The tracker fires the turn-error hook with the extracted error text for
// both protocols' turn-failure shapes.
func TestNudgeTrackerTurnErrorFires(t *testing.T) {
	cases := []struct {
		name     string
		proto    string
		lines    []string
		wantText string
	}{
		{
			name:  "claude error result",
			proto: ProtocolClaude,
			lines: []string{
				`{"type":"user","message":{"role":"user","content":"hi"}}`,
				`{"type":"assistant","message":{"role":"assistant"}}`,
				`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Invalid API key. Please run /login"}`,
			},
			wantText: "Invalid API key. Please run /login",
		},
		{
			name:  "codex failed turn",
			proto: ProtocolCodex,
			lines: []string{
				`{"method":"turn/started","params":{"threadId":"t1","turn":{"id":"turn1"}}}`,
				`{"method":"turn/completed","params":{"threadId":"t1","turnId":"turn1","turn":{"id":"turn1","status":"failed","error":{"message":"Codex is not logged in"}}}}`,
			},
			wantText: "Codex is not logged in",
		},
		{
			name:  "codex error event no retry",
			proto: ProtocolCodex,
			lines: []string{
				`{"method":"turn/started","params":{"threadId":"t1","turn":{"id":"turn1"}}}`,
				`{"method":"error","params":{"threadId":"t1","turnId":"turn1","error":{"message":"login required"},"willRetry":false}}`,
			},
			wantText: "login required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewNudgeTracker(tc.proto)
			var got string
			var fired bool
			tr.onTurnError = func(text string) { got, fired = text, true }
			for _, line := range tc.lines {
				tr.Rec(NewHarness([]byte(line)))
			}
			if !fired {
				t.Fatal("turn-error hook did not fire")
			}
			if got != tc.wantText {
				t.Errorf("hook text = %q, want %q", got, tc.wantText)
			}
		})
	}
}

// Successful results and replayed history never fire the hook.
func TestNudgeTrackerTurnErrorSilent(t *testing.T) {
	t.Run("claude success result does not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(string) { fired = true }
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)))
		if fired {
			t.Error("hook fired on a successful result")
		}
	})
	t.Run("replaying records do not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(string) { fired = true }
		tr.replaying = true
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Please run /login"}`)))
		tr.replaying = false
		if fired {
			t.Error("hook fired during replay")
		}
	})
}
