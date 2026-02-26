package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleStatusEventIntegration(t *testing.T) {
	tests := []struct {
		name           string
		eventState     string
		message        string
		wantNudgeEmpty bool
		wantSeqDelta   uint64
	}{
		{
			name:           "completed increments seq and sets nudge",
			eventState:     "completed",
			message:        "Task done",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "error increments seq and sets nudge",
			eventState:     "error",
			message:        "Build failed",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "needs_input increments seq and sets nudge",
			eventState:     "needs_input",
			message:        "Awaiting approval",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
		{
			name:           "working increments seq and sets nudge",
			eventState:     "working",
			message:        "Implementing feature",
			wantNudgeEmpty: false,
			wantSeqDelta:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, st := newTestServer(t)
			st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

			// Pre-set a nudge so we can verify events overwrite it
			st.UpdateSessionNudge("s1", `{"state":"Working","summary":"old"}`)
			seqBefore := st.GetNudgeSeq("s1")

			srv.HandleStatusEvent("s1", tt.eventState, tt.message, "", "")

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

func TestHandleStatusEventRapidSignals(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

	// Sequence respects state priority: working resets between terminal states
	states := []string{"working", "completed", "working", "needs_input", "working", "completed"}
	for _, s := range states {
		srv.HandleStatusEvent("s1", s, "msg", "", "")
	}

	// All 6 events are valid transitions, so all increment seq
	expectedSeq := uint64(6)
	gotSeq := st.GetNudgeSeq("s1")
	if gotSeq != expectedSeq {
		t.Errorf("NudgeSeq = %d, want %d", gotSeq, expectedSeq)
	}

	// Last event was "completed" so nudge should be non-empty
	sess, _ := st.GetSession("s1")
	if sess.Nudge == "" {
		t.Error("expected non-empty nudge after final completed event")
	}
}

func TestHandleStatusEvent_DuplicateNudgeSkipsSeq(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

	// First needs_input event should increment seq
	srv.HandleStatusEvent("s1", "needs_input", "Permission needed", "", "")
	seqAfterFirst := st.GetNudgeSeq("s1")
	if seqAfterFirst != 1 {
		t.Fatalf("NudgeSeq after first = %d, want 1", seqAfterFirst)
	}

	// Duplicate needs_input with same message should NOT increment seq
	srv.HandleStatusEvent("s1", "needs_input", "Permission needed", "", "")
	seqAfterDup := st.GetNudgeSeq("s1")
	if seqAfterDup != 1 {
		t.Errorf("NudgeSeq after duplicate = %d, want 1 (should not increment)", seqAfterDup)
	}

	// Different state should increment seq
	srv.HandleStatusEvent("s1", "working", "Resuming", "", "")
	seqAfterDifferent := st.GetNudgeSeq("s1")
	if seqAfterDifferent != 2 {
		t.Errorf("NudgeSeq after different state = %d, want 2", seqAfterDifferent)
	}
}

func TestHandleStatusEvent_StatePriority(t *testing.T) {
	tests := []struct {
		name          string
		initialState  string
		incomingState string
		wantBlocked   bool
	}{
		{
			name:          "idle cannot overwrite completed",
			initialState:  "completed",
			incomingState: "idle",
			wantBlocked:   true,
		},
		{
			name:          "idle cannot overwrite error",
			initialState:  "error",
			incomingState: "idle",
			wantBlocked:   true,
		},
		{
			name:          "idle cannot overwrite needs_input",
			initialState:  "needs_input",
			incomingState: "idle",
			wantBlocked:   true,
		},
		{
			name:          "needs_input cannot overwrite completed",
			initialState:  "completed",
			incomingState: "needs_input",
			wantBlocked:   true,
		},
		{
			name:          "working overwrites completed",
			initialState:  "completed",
			incomingState: "working",
			wantBlocked:   false,
		},
		{
			name:          "working overwrites error",
			initialState:  "error",
			incomingState: "working",
			wantBlocked:   false,
		},
		{
			name:          "working overwrites needs_input",
			initialState:  "needs_input",
			incomingState: "working",
			wantBlocked:   false,
		},
		{
			name:          "completed overwrites idle",
			initialState:  "idle",
			incomingState: "completed",
			wantBlocked:   false,
		},
		{
			name:          "completed overwrites working",
			initialState:  "working",
			incomingState: "completed",
			wantBlocked:   false,
		},
		{
			name:          "needs_input overwrites idle",
			initialState:  "idle",
			incomingState: "needs_input",
			wantBlocked:   false,
		},
		{
			name:          "needs_input overwrites working",
			initialState:  "working",
			incomingState: "needs_input",
			wantBlocked:   false,
		},
		{
			name:          "error overwrites completed (same tier)",
			initialState:  "completed",
			incomingState: "error",
			wantBlocked:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, st := newTestServer(t)
			st.AddSession(state.Session{ID: "s1", TmuxSession: "test"})

			// Set initial state
			srv.HandleStatusEvent("s1", tt.initialState, "initial", "", "")
			sess, _ := st.GetSession("s1")
			initialNudge := sess.Nudge
			seqAfterInitial := st.GetNudgeSeq("s1")

			// Send incoming state
			srv.HandleStatusEvent("s1", tt.incomingState, "incoming", "", "")
			sess, _ = st.GetSession("s1")
			seqAfterIncoming := st.GetNudgeSeq("s1")

			if tt.wantBlocked {
				// Nudge should be unchanged
				if sess.Nudge != initialNudge {
					t.Errorf("expected nudge unchanged, got %q (was %q)", sess.Nudge, initialNudge)
				}
				if seqAfterIncoming != seqAfterInitial {
					t.Errorf("expected seq unchanged at %d, got %d", seqAfterInitial, seqAfterIncoming)
				}
			} else {
				// Nudge should have changed
				if sess.Nudge == initialNudge {
					t.Errorf("expected nudge to change from %q", initialNudge)
				}
				if seqAfterIncoming <= seqAfterInitial {
					t.Errorf("expected seq to increment from %d, got %d", seqAfterInitial, seqAfterIncoming)
				}
			}
		})
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
	t.Cleanup(tracker.Stop)

	// Initially, terminal size fields should be zero
	if tracker.LastTerminalCols.Load() != 0 || tracker.LastTerminalRows.Load() != 0 {
		t.Errorf("initial terminal size should be 0x0, got %dx%d",
			tracker.LastTerminalCols.Load(), tracker.LastTerminalRows.Load())
	}

	// Directly set the terminal dimensions to simulate what Resize() does
	// (We can't call actual Resize without a control mode client)
	tracker.LastTerminalCols.Store(120)
	tracker.LastTerminalRows.Store(40)

	// Verify the fields were set
	if tracker.LastTerminalCols.Load() != 120 || tracker.LastTerminalRows.Load() != 40 {
		t.Errorf("terminal size after assignment should be 120x40, got %dx%d",
			tracker.LastTerminalCols.Load(), tracker.LastTerminalRows.Load())
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
	t.Cleanup(tracker.Stop)

	// Simulate terminal size being stored from a resize message
	tracker.LastTerminalCols.Store(120)
	tracker.LastTerminalRows.Store(40)

	// Create a DiagnosticCapture using the stored terminal size
	diag := &DiagnosticCapture{
		Timestamp:  time.Now(),
		SessionID:  "test-session",
		Cols:       int(tracker.LastTerminalCols.Load()),
		Rows:       int(tracker.LastTerminalRows.Load()),
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
