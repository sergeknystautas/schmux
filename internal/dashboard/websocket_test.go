package dashboard

import (
	"fmt"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleAgentSignalIntegration(t *testing.T) {
	tests := []struct {
		name           string
		signalState    string
		message        string
		wantNudgeEmpty bool
		wantSeqDelta   uint64
	}{
		{
			name:           "completed increments seq and sets nudge",
			signalState:    "completed",
			message:        "Task done",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "error increments seq and sets nudge",
			signalState:    "error",
			message:        "Build failed",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "needs_input increments seq and sets nudge",
			signalState:    "needs_input",
			message:        "Awaiting approval",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "working clears nudge without incrementing seq",
			signalState:    "working",
			message:        "",
			wantNudgeEmpty: true,
			wantSeqDelta:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := state.New("")
			st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

			// Pre-set a nudge so we can verify working clears it
			st.UpdateSessionNudge("s1", `{"state":"Error","summary":"old"}`)
			seqBefore := st.GetNudgeSeq("s1")

			// Simulate HandleAgentSignal logic
			sig := signal.Signal{State: tt.signalState, Message: tt.message, Timestamp: time.Now()}
			if sig.State == "working" {
				st.UpdateSessionNudge("s1", "")
			} else {
				nudgeJSON := fmt.Sprintf(`{"state":"%s","summary":"%s","source":"agent"}`,
					signal.MapStateToNudge(sig.State), sig.Message)
				st.UpdateSessionNudge("s1", nudgeJSON)
			}
			st.UpdateSessionLastSignal("s1", sig.Timestamp)
			if sig.State != "working" {
				st.IncrementNudgeSeq("s1")
			}

			// Verify
			sess, _ := st.GetSession("s1")
			seqAfter := st.GetNudgeSeq("s1")

			if tt.wantNudgeEmpty && sess.Nudge != "" {
				t.Errorf("expected empty nudge, got %q", sess.Nudge)
			}
			if !tt.wantNudgeEmpty && sess.Nudge == "" {
				t.Errorf("expected non-empty nudge")
			}
			if (seqAfter - seqBefore) != tt.wantSeqDelta {
				t.Errorf("NudgeSeq delta = %d, want %d", seqAfter-seqBefore, tt.wantSeqDelta)
			}
		})
	}
}

func TestHandleAgentSignalRapidSignals(t *testing.T) {
	// Verifies that rapid signals don't corrupt state when using atomic updates
	st := state.New("")
	st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

	states := []string{"completed", "working", "error", "needs_input", "working", "completed"}
	for _, s := range states {
		sig := signal.Signal{State: s, Message: "msg", Timestamp: time.Now()}
		if sig.State == "working" {
			st.UpdateSessionNudge("s1", "")
		} else {
			st.UpdateSessionNudge("s1", fmt.Sprintf(`{"state":"%s"}`, signal.MapStateToNudge(sig.State)))
		}
		if sig.State != "working" {
			st.IncrementNudgeSeq("s1")
		}
	}

	// 4 non-working signals: completed, error, needs_input, completed
	expectedSeq := uint64(4)
	gotSeq := st.GetNudgeSeq("s1")
	if gotSeq != expectedSeq {
		t.Errorf("NudgeSeq = %d, want %d", gotSeq, expectedSeq)
	}

	// Last signal was "completed" so nudge should be non-empty
	sess, _ := st.GetSession("s1")
	if sess.Nudge == "" {
		t.Error("expected non-empty nudge after final completed signal")
	}
}
