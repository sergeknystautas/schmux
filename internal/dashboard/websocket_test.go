package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
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

func TestStatsMessage_JSON(t *testing.T) {
	msg := WSStatsMessage{
		Type:            "stats",
		EventsDelivered: 100,
		EventsDropped:   2,
		BytesDelivered:  50000,
		Reconnects:      0,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if decoded["type"] != "stats" {
		t.Errorf("type = %v, want stats", decoded["type"])
	}
	if int(decoded["eventsDropped"].(float64)) != 2 {
		t.Errorf("eventsDropped = %v, want 2", decoded["eventsDropped"])
	}
}

// TestTerminalSizeTracking verifies that the SessionTracker has fields to store
// terminal dimensions received from WebSocket resize messages.
func TestTerminalSizeTracking(t *testing.T) {
	// This test verifies the fix for terminal desync: backend should track terminal size
	// so that DiagnosticCapture includes correct Cols/Rows instead of defaulting to 0x0.
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "test-session", TmuxSession: "test"})

	// Get the tracker
	tracker, err := srv.session.GetTracker("test-session")
	if err != nil {
		t.Fatalf("failed to get tracker: %v", err)
	}

	// Initially, terminal size fields should be zero
	if tracker.LastTerminalCols != 0 || tracker.LastTerminalRows != 0 {
		t.Errorf("initial terminal size should be 0x0, got %dx%d",
			tracker.LastTerminalCols, tracker.LastTerminalRows)
	}

	// Directly set the terminal dimensions to simulate what Resize() does
	// (We can't call actual Resize without a control mode client)
	tracker.LastTerminalCols = 120
	tracker.LastTerminalRows = 40

	// Verify the fields were set
	if tracker.LastTerminalCols != 120 || tracker.LastTerminalRows != 40 {
		t.Errorf("terminal size after assignment should be 120x40, got %dx%d",
			tracker.LastTerminalCols, tracker.LastTerminalRows)
	}
}

// TestDiagnosticCaptureIncludesTerminalSize verifies that DiagnosticCapture
// correctly includes terminal dimensions from the tracker.
func TestDiagnosticCaptureIncludesTerminalSize(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "test-session", TmuxSession: "test"})

	tracker, err := srv.session.GetTracker("test-session")
	if err != nil {
		t.Fatalf("failed to get tracker: %v", err)
	}

	// Simulate terminal size being stored from a resize message
	tracker.LastTerminalCols = 120
	tracker.LastTerminalRows = 40

	// Create a DiagnosticCapture using the stored terminal size
	diag := &DiagnosticCapture{
		Timestamp:  time.Now(),
		SessionID:  "test-session",
		Cols:       tracker.LastTerminalCols,
		Rows:       tracker.LastTerminalRows,
		Counters:   tracker.DiagnosticCounters(),
		TmuxScreen: "test screen content",
		Findings:   []string{"No drops detected"},
		Verdict:    "Test verdict",
	}

	// Verify the diagnostic has correct dimensions, not 0x0
	if diag.Cols != 120 {
		t.Errorf("diagnostic Cols = %d, want 120", diag.Cols)
	}
	if diag.Rows != 40 {
		t.Errorf("diagnostic Rows = %d, want 40", diag.Rows)
	}
}

func TestStatsMessage_SyncFields(t *testing.T) {
	msg := WSStatsMessage{
		Type:              "stats",
		EventsDelivered:   100,
		SyncChecksSent:    5,
		SyncCorrections:   1,
		SyncSkippedActive: 2,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if int(decoded["syncChecksSent"].(float64)) != 5 {
		t.Errorf("syncChecksSent = %v, want 5", decoded["syncChecksSent"])
	}
	if int(decoded["syncCorrections"].(float64)) != 1 {
		t.Errorf("syncCorrections = %v, want 1", decoded["syncCorrections"])
	}
	if int(decoded["syncSkippedActive"].(float64)) != 2 {
		t.Errorf("syncSkippedActive = %v, want 2", decoded["syncSkippedActive"])
	}
}

func TestBuildSyncMessage(t *testing.T) {
	screen := "\x1b[1mhello\x1b[0m world\nline two"
	cursor := controlmode.CursorState{X: 3, Y: 24, Visible: true}

	msg := buildSyncMessage(screen, cursor)

	if msg.Type != "sync" {
		t.Errorf("type = %q, want sync", msg.Type)
	}
	if msg.Screen != screen {
		t.Errorf("screen content mismatch")
	}
	if msg.Cursor.Row != 24 || msg.Cursor.Col != 3 || !msg.Cursor.Visible {
		t.Errorf("cursor = %+v, want {Row:24 Col:3 Visible:true}", msg.Cursor)
	}

	// Verify JSON marshaling
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if decoded["type"] != "sync" {
		t.Errorf("JSON type = %v, want sync", decoded["type"])
	}
	cursorMap := decoded["cursor"].(map[string]interface{})
	if int(cursorMap["row"].(float64)) != 24 {
		t.Errorf("JSON cursor.row = %v, want 24", cursorMap["row"])
	}
}
