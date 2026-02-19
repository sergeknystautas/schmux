package dashboard

import (
	"encoding/json"
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
			name:           "working increments seq and sets nudge",
			signalState:    "working",
			message:        "Implementing feature",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, st := newTestServer(t)
			st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

			// Pre-set a nudge so we can verify signals overwrite it
			st.UpdateSessionNudge("s1", `{"state":"Error","summary":"old"}`)
			seqBefore := st.GetNudgeSeq("s1")

			sig := signal.Signal{State: tt.signalState, Message: tt.message, Timestamp: time.Now()}
			srv.HandleAgentSignal("s1", sig)

			// Verify
			sess, _ := st.GetSession("s1")
			seqAfter := st.GetNudgeSeq("s1")

			if tt.wantNudgeEmpty && sess.Nudge != "" {
				t.Errorf("expected empty nudge, got %q", sess.Nudge)
			}
			if !tt.wantNudgeEmpty && sess.Nudge == "" {
				t.Errorf("expected non-empty nudge")
			}
			if !tt.wantNudgeEmpty {
				// Verify the nudge payload is valid JSON with expected fields
				var nudge map[string]string
				if err := json.Unmarshal([]byte(sess.Nudge), &nudge); err != nil {
					t.Errorf("nudge is not valid JSON: %v", err)
				} else {
					if nudge["source"] != "agent" {
						t.Errorf("nudge source = %q, want %q", nudge["source"], "agent")
					}
				}
			}
			if (seqAfter - seqBefore) != tt.wantSeqDelta {
				t.Errorf("NudgeSeq delta = %d, want %d", seqAfter-seqBefore, tt.wantSeqDelta)
			}
		})
	}
}

func TestFilterMouseMode_EraseDisplayBecomesScrollUp(t *testing.T) {
	// \x1b[2J (Erase Display) from tmux full-screen redraws should be
	// converted to \x1b[999S (Scroll Up) so viewport content is pushed
	// into scrollback instead of being erased.
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "standalone erase display",
			input: []byte("\x1b[2J"),
			want:  "\x1b[999S",
		},
		{
			name:  "cursor home + erase display (tmux redraw pattern)",
			input: []byte("\x1b[H\x1b[2J"),
			want:  "\x1b[H\x1b[999S",
		},
		{
			name:  "erase display embedded in output",
			input: []byte("before\x1b[2Jafter"),
			want:  "before\x1b[999Safter",
		},
		{
			name:  "multiple erase display sequences",
			input: []byte("\x1b[2Jfoo\x1b[2Jbar"),
			want:  "\x1b[999Sfoo\x1b[999Sbar",
		},
		{
			name:  "no erase display passes through",
			input: []byte("hello world"),
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterMouseMode(tt.input)
			if string(got) != tt.want {
				t.Errorf("filterMouseMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFilterMouseMode_EraseScrollbackStripped(t *testing.T) {
	// \x1b[3J (Erase Scrollback) should be stripped entirely to
	// prevent tmux from clearing xterm.js scrollback buffer.
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "standalone erase scrollback",
			input: []byte("\x1b[3J"),
			want:  "",
		},
		{
			name:  "erase scrollback in output",
			input: []byte("before\x1b[3Jafter"),
			want:  "beforeafter",
		},
		{
			name:  "both erase display and erase scrollback",
			input: []byte("\x1b[2J\x1b[3J"),
			want:  "\x1b[999S",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterMouseMode(tt.input)
			if string(got) != tt.want {
				t.Errorf("filterMouseMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleAgentSignalRapidSignals(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

	states := []string{"completed", "working", "error", "needs_input", "working", "completed"}
	for _, s := range states {
		sig := signal.Signal{State: s, Message: "msg", Timestamp: time.Now()}
		srv.HandleAgentSignal("s1", sig)
	}

	// All 6 signals increment seq (including working)
	expectedSeq := uint64(6)
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
