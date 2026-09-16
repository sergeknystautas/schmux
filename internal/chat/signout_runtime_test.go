package chat

import "testing"

// The tracker fires the turn-error hook with the extracted error text for
// both protocols' turn-failure shapes.
func TestNudgeTrackerTurnErrorFires(t *testing.T) {
	cases := []struct {
		name          string
		proto         string
		lines         []string
		wantText      string
		wantAPIStatus int
	}{
		{
			name:  "claude rejected credential",
			proto: ProtocolClaude,
			lines: []string{
				`{"type":"user","message":{"role":"user","content":"hi"}}`,
				`{"type":"assistant","error":"authentication_failed","is_api_error_message":true,"message":{"role":"assistant","content":[{"type":"text","text":"Failed to authenticate. API Error: 401 OAuth access token has been revoked."}]}}`,
				`{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","api_error_status":401,"result":"Failed to authenticate. API Error: 401 OAuth access token has been revoked."}`,
			},
			wantText:      "Failed to authenticate. API Error: 401 OAuth access token has been revoked.",
			wantAPIStatus: 401,
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
			var got turnError
			var fired bool
			tr.onTurnError = func(turnErr turnError) { got, fired = turnErr, true }
			for _, line := range tc.lines {
				tr.Rec(NewHarness([]byte(line)))
			}
			if !fired {
				t.Fatal("turn-error hook did not fire")
			}
			if got.text != tc.wantText || got.apiErrorStatus != tc.wantAPIStatus {
				t.Errorf("hook error = %+v, want text %q and API status %d", got, tc.wantText, tc.wantAPIStatus)
			}
		})
	}
}

// Successful results and replayed history never fire the hook.
func TestNudgeTrackerTurnErrorSilent(t *testing.T) {
	t.Run("claude success result does not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(turnError) { fired = true }
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)))
		if fired {
			t.Error("hook fired on a successful result")
		}
	})
	t.Run("replaying records do not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(turnError) { fired = true }
		tr.replaying = true
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Please run /login"}`)))
		tr.replaying = false
		if fired {
			t.Error("hook fired during replay")
		}
	})
}
