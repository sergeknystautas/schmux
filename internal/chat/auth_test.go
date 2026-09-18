package chat

import (
	"reflect"
	"testing"
)

func TestCodexAccountConsumersAgree(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		ready     bool
		status    string
		signedOut bool
	}{
		{"ChatGPT", `{"id":2,"result":{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}}`, true, "Idle", false},
		{"API key", `{"id":2,"result":{"account":{"type":"apiKey"}}}`, true, "Idle", false},
		{"account object without type", `{"id":2,"result":{"account":{"id":"acct"}}}`, true, "Idle", false},
		{"provider", `{"id":2,"result":{"account":null,"requiresOpenaiAuth":false}}`, true, "Idle", false},
		{"logged out", `{"id":2,"result":{"account":null,"requiresOpenaiAuth":true}}`, false, "Error", true},
		{"legacy logged out", `{"id":2,"result":{"account":null}}`, false, "Error", true},
		{"missing account", `{"id":2,"result":{}}`, false, "Idle", false},
		{"malformed account", `{"id":2,"result":{"account":"unexpected"}}`, false, "Idle", false},
		{"RPC failure", `{"id":2,"error":{"message":"transport unavailable"}}`, false, "Error", false},
		{"null RPC error", `{"id":2,"error":null,"result":{"account":{"type":"chatgpt"}}}`, true, "Idle", false},
		{"server request namespace", `{"id":2,"method":"item/tool/requestUserInput","params":{}}`, false, "Needs Input", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newCodexProtocol()
			p.Observe([]byte(`{"id":3,"result":{"thread":{"id":"t"}}}`))
			p.Observe([]byte(tc.line))
			tracker := NewNudgeTracker(ProtocolCodex)
			var failures []turnError
			tracker.onTurnError = func(e turnError) { failures = append(failures, e) }
			tracker.Rec(NewHarness([]byte(tc.line)))
			if p.Addressable() != tc.ready || tracker.Result().State != tc.status || (len(failures) > 0) != tc.signedOut {
				t.Fatalf("ready=%v status=%+v auth failures=%+v; want ready=%v status=%s signedOut=%v", p.Addressable(), tracker.Result(), failures, tc.ready, tc.status, tc.signedOut)
			}
			// Rebuilding a transcript may restore status, but cannot initiate
			// login recovery from an old account response.
			tracker.replaying = true
			failures = nil
			tracker.Rec(NewHarness([]byte(tc.line)))
			if len(failures) != 0 {
				t.Fatalf("historical auth triggered recovery: %+v", failures)
			}
		})
	}
}

func TestCodexProviderAuthAgreesWithStatus(t *testing.T) {
	// Captured from the restarted GLM sessions: a provider session needs no
	// OpenAI account, and its resumed thread is idle.
	lines := []string{
		`{"id":2,"result":{"account":null,"requiresOpenaiAuth":false}}`,
		`{"id":3,"result":{"thread":{"id":"provider-thread","status":{"type":"idle"}}}}`,
	}
	p := newCodexProtocol()
	tracker := NewNudgeTracker(ProtocolCodex)
	for _, line := range lines {
		p.Observe([]byte(line))
		tracker.Rec(NewHarness([]byte(line)))
	}
	if !p.Addressable() || tracker.Result().State != "Idle" {
		t.Fatalf("provider startup: addressable=%v status=%+v", p.Addressable(), tracker.Result())
	}
}

func TestRuntimeAuthRecoveryIsLiveOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		account    string
		wantState  string
		wantErrors int
	}{
		{"provider", `{"account":null,"requiresOpenaiAuth":false}`, "Idle", 0},
		{"logged out", `{"account":null,"requiresOpenaiAuth":true}`, "Error", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := PathsFor(t.TempDir())
			if err := paths.Ensure(); err != nil {
				t.Fatal(err)
			}
			live, err := NewRuntime("s", newCodexProtocol(), paths, "", "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(live.Stop)
			var failures []TurnErrorEvent
			live.SetTurnErrorCallback(func(e TurnErrorEvent) { failures = append(failures, e) })
			writeOutput(t, paths, `{"id":2,"result":`+tc.account+`}`, `{"id":3,"result":{"thread":{"id":"t"}}}`)
			// drain is synchronous: its return is the boundary for both record
			// persistence and status/recovery delivery, with no polling.
			live.drain()
			if got := live.nudgeTracker.Result(); got.State != tc.wantState || len(failures) != tc.wantErrors {
				t.Fatalf("live status=%+v recovery=%+v; want %s and %d errors", got, failures, tc.wantState, tc.wantErrors)
			}
			history, err := live.log.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 2 {
				t.Fatalf("history=%+v", history)
			}
			restored, err := NewRuntime("s", newCodexProtocol(), paths, "", "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Stop)
			failures = nil
			restored.SetTurnErrorCallback(func(e TurnErrorEvent) { failures = append(failures, e) })
			restored.Start()
			restored.Stop()
			if got := restored.nudgeTracker.Result(); got.State != tc.wantState || len(failures) != 0 {
				t.Fatalf("restored status=%+v recovery=%+v; want %s without recovery", got, failures, tc.wantState)
			}
			after, err := restored.log.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(history, after) {
				t.Fatalf("history changed: before=%+v after=%+v", history, after)
			}
		})
	}
}
