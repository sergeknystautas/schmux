package chat

import "testing"

func TestNudgeCodexStartupErrorSurvivesQueuedMessages(t *testing.T) {
	for _, messageFirst := range []bool{false, true} {
		name := "error before message"
		if messageFirst {
			name = "message before error"
		}
		t.Run(name, func(t *testing.T) {
			tr := NewNudgeTracker(ProtocolCodex)
			if messageFirst {
				tr.Rec(recUser("hi"))
			}
			tr.Rec(recHarness([]byte(`{"id":3,"error":{"code":-32600,"message":"thread already has an active writer"}}`)))
			want := Nudge{State: "Error", Summary: "thread already has an active writer", Source: "headless"}
			if got := tr.Result(); got != want {
				t.Fatalf("startup failure = %+v, want %+v", got, want)
			}
			tr.Rec(recUser("are you there?"))
			if got := tr.Result(); got != want {
				t.Fatalf("after queued message = %+v, want %+v", got, want)
			}
			tr.Rec(recHarness([]byte(`{"id":3,"result":{"thread":{"id":"thread-1"}}}`)))
			tr.Rec(recHarness([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`)))
			if got := tr.Result(); got.State != "Working" || got.Summary != "" {
				t.Fatalf("after turn starts = %+v, want Working without an error", got)
			}
		})
	}
}
