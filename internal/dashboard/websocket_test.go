package dashboard

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/session"
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

// TestTerminalSizeTracking verifies that Resize() validates dimensions and
// stores them for diagnostic capture, even when no control mode client is attached.
func TestTerminalSizeTracking(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "test-session", TmuxSession: "test"})

	tracker, err := srv.session.GetTracker("test-session")
	if err != nil {
		t.Fatalf("failed to get tracker: %v", err)
	}
	t.Cleanup(tracker.Stop)

	// Initially, terminal size should be zero
	if tracker.LastTerminalCols.Load() != 0 || tracker.LastTerminalRows.Load() != 0 {
		t.Errorf("initial terminal size should be 0x0, got %dx%d",
			tracker.LastTerminalCols.Load(), tracker.LastTerminalRows.Load())
	}

	// Resize stores dimensions even without a control mode client (returns "not attached")
	err = tracker.Resize(120, 40)
	if err == nil {
		t.Error("Resize without control mode client should return error")
	}
	if tracker.LastTerminalCols.Load() != 120 || tracker.LastTerminalRows.Load() != 40 {
		t.Errorf("Resize should store dimensions even on error, got %dx%d",
			tracker.LastTerminalCols.Load(), tracker.LastTerminalRows.Load())
	}

	// Invalid dimensions should be rejected without changing stored values
	err = tracker.Resize(0, 40)
	if err == nil {
		t.Error("Resize(0, 40) should return error for invalid cols")
	}
	if tracker.LastTerminalCols.Load() != 120 {
		t.Errorf("invalid Resize should not change stored cols, got %d", tracker.LastTerminalCols.Load())
	}

	err = tracker.Resize(80, -1)
	if err == nil {
		t.Error("Resize(80, -1) should return error for invalid rows")
	}
	if tracker.LastTerminalRows.Load() != 40 {
		t.Errorf("invalid Resize should not change stored rows, got %d", tracker.LastTerminalRows.Load())
	}
}

// TestDiagnosticCaptureIncludesTerminalSize verifies that DiagnosticCapture
// built from tracker state after Resize() has correct dimensions (not 0x0).
func TestDiagnosticCaptureIncludesTerminalSize(t *testing.T) {
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "test-session", TmuxSession: "test"})

	tracker, err := srv.session.GetTracker("test-session")
	if err != nil {
		t.Fatalf("failed to get tracker: %v", err)
	}
	t.Cleanup(tracker.Stop)

	// Before any resize, diagnostic should show 0x0
	diag := &DiagnosticCapture{
		Cols: int(tracker.LastTerminalCols.Load()),
		Rows: int(tracker.LastTerminalRows.Load()),
	}
	if diag.Cols != 0 || diag.Rows != 0 {
		t.Errorf("diagnostic before resize should be 0x0, got %dx%d", diag.Cols, diag.Rows)
	}

	// After Resize(), diagnostic built the same way as production code
	// (websocket.go:907-908) should reflect the new dimensions.
	_ = tracker.Resize(120, 40) // error expected (no cmClient), but dimensions stored

	diag = &DiagnosticCapture{
		Timestamp:  time.Now(),
		SessionID:  "test-session",
		Cols:       int(tracker.LastTerminalCols.Load()),
		Rows:       int(tracker.LastTerminalRows.Load()),
		Counters:   tracker.DiagnosticCounters(),
		TmuxScreen: "test screen content",
		Findings:   []string{"No drops detected"},
		Verdict:    "Test verdict",
	}

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

func TestMapEventStateToNudge(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"needs_input", "Needs Input"},
		{"needs_testing", "Needs Attention"},
		{"completed", "Completed"},
		{"error", "Error"},
		{"working", "Working"},
		{"idle", "Idle"},
		{"unknown_state", "unknown_state"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapEventStateToNudge(tt.input)
			if got != tt.want {
				t.Errorf("mapEventStateToNudge(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNudgeStateTier(t *testing.T) {
	tests := []struct {
		displayState string
		wantTier     int
	}{
		{"Working", 0},
		{"Idle", 0},
		{"something_unknown", 0},
		{"Needs Input", 1},
		{"Needs Attention", 1},
		{"Needs Feature Clarification", 1},
		{"Completed", 2},
		{"Error", 2},
	}

	for _, tt := range tests {
		t.Run(tt.displayState, func(t *testing.T) {
			got := nudgeStateTier(tt.displayState)
			if got != tt.wantTier {
				t.Errorf("nudgeStateTier(%q) = %d, want %d", tt.displayState, got, tt.wantTier)
			}
		})
	}

	// Verify ordering: tier 2 > tier 1 > tier 0
	if nudgeStateTier("Completed") <= nudgeStateTier("Needs Input") {
		t.Error("terminal tier should be higher than blocking tier")
	}
	if nudgeStateTier("Needs Input") <= nudgeStateTier("Working") {
		t.Error("blocking tier should be higher than transient tier")
	}
}

func TestIsTerminalResponse(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"DA1 response", "\x1b[?1;2c", true},
		{"DA2 response", "\x1b[>0;276;0c", true},
		{"OSC 10 foreground", "\x1b]10;rgb:ff/ff/ff\x1b\\", true},
		{"OSC 11 background", "\x1b]11;rgb:00/00/00\x1b\\", true},
		{"normal input", "hello world", false},
		{"escape but not response", "\x1b[A", false},
		{"empty string", "", false},
		{"single escape", "\x1b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTerminalResponse(tt.data)
			if got != tt.want {
				t.Errorf("isTerminalResponse(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
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

func TestAppendSequencedFrame(t *testing.T) {
	data := []byte("hello")
	frame := appendSequencedFrame(nil, 42, data)

	if len(frame) != 8+5 {
		t.Fatalf("frame length=%d, want 13", len(frame))
	}

	// Verify big-endian uint64 sequence
	seq := binary.BigEndian.Uint64(frame[:8])
	if seq != 42 {
		t.Errorf("seq=%d, want 42", seq)
	}
	if string(frame[8:]) != "hello" {
		t.Errorf("data=%q, want 'hello'", frame[8:])
	}
}

func TestAppendSequencedFrame_Reuse(t *testing.T) {
	data := []byte("hello")
	buf := make([]byte, 0, 128)

	buf = appendSequencedFrame(buf, 1, data)
	if len(buf) != 13 {
		t.Fatalf("frame length=%d, want 13", len(buf))
	}

	// Reuse should not allocate — same backing array
	ptr1 := &buf[0]
	buf = appendSequencedFrame(buf, 2, data)
	ptr2 := &buf[0]
	if ptr1 != ptr2 {
		t.Error("appendSequencedFrame allocated when capacity was sufficient")
	}

	seq := binary.BigEndian.Uint64(buf[:8])
	if seq != 2 {
		t.Errorf("seq=%d, want 2", seq)
	}
}

func TestBuildGapReplayFrames(t *testing.T) {
	log := session.NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c"))

	frames := buildGapReplayFrames(log, 1) // replay from seq 1
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames (one per entry), got %d", len(frames))
	}
	// Each frame should carry its own entry's seq and data
	if string(frames[0][8:]) != "b" {
		t.Errorf("frame 0 data=%q, want 'b'", frames[0][8:])
	}
	if string(frames[1][8:]) != "c" {
		t.Errorf("frame 1 data=%q, want 'c'", frames[1][8:])
	}
	// Verify seq headers are individual entry seqs (1 and 2)
	seq0 := binary.BigEndian.Uint64(frames[0][:8])
	seq1 := binary.BigEndian.Uint64(frames[1][:8])
	if seq0 != 1 || seq1 != 2 {
		t.Errorf("seqs=%d,%d, want 1,2", seq0, seq1)
	}
}

func TestBuildGapReplayFrames_EvictedData(t *testing.T) {
	log := session.NewOutputLog(2) // tiny buffer
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c")) // evicts "a"

	// Request from seq 0 (evicted) — should return nil
	frames := buildGapReplayFrames(log, 0)
	if frames != nil {
		t.Errorf("expected nil for evicted data, got %d frames", len(frames))
	}
}

// TestBootstrapSeqDoesNotCollideWithFirstLiveEvent verifies that the bootstrap
// frame's sequence number is strictly less than the first live output event's
// sequence number. If they collide, the frontend's dedup logic drops the first
// live event (the echo of the user's first keystroke).
func TestBootstrapSeqDoesNotCollideWithFirstLiveEvent(t *testing.T) {
	log := session.NewOutputLog(100)

	// Simulate some prior output (agent produced output before WebSocket connects)
	log.Append([]byte("prior output 1"))
	log.Append([]byte("prior output 2"))

	// This is what the WebSocket handler does at bootstrap time:
	bootstrapSeq := bootstrapFrameSeq(log)

	// This is what happens when the user types and the echo arrives:
	firstLiveSeq := log.Append([]byte("first keystroke echo"))

	if bootstrapSeq >= firstLiveSeq {
		t.Errorf("bootstrap frame seq (%d) must be < first live event seq (%d); "+
			"equal values cause the frontend dedup to drop the first keystroke echo",
			bootstrapSeq, firstLiveSeq)
	}
}

func TestBootstrapSeqDoesNotCollideWithFirstLiveEvent_EmptyLog(t *testing.T) {
	log := session.NewOutputLog(100)

	// Empty log: no prior output (freshly spawned session)
	bootstrapSeq := bootstrapFrameSeq(log)

	firstLiveSeq := log.Append([]byte("first keystroke echo"))

	if bootstrapSeq >= firstLiveSeq {
		t.Errorf("bootstrap frame seq (%d) must be < first live event seq (%d); "+
			"equal values cause the frontend dedup to drop the first keystroke echo",
			bootstrapSeq, firstLiveSeq)
	}
}

func TestBuildDiagnosticFindings(t *testing.T) {
	tests := []struct {
		name            string
		counters        map[string]int64
		wantFindings    []string // substrings each finding must contain
		notWantFindings []string // substrings that must NOT appear in any finding
		wantVerdict     string   // substring the verdict must contain
	}{
		{
			name: "all zeros — no issues",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     0,
				"fanOutDrops":           0,
				"controlModeReconnects": 0,
				"currentSeq":            100,
				"logOldestSeq":          1,
			},
			wantFindings:    []string{"No drops or anomalies detected"},
			notWantFindings: []string{"dropped", "reconnect", "capacity"},
			wantVerdict:     "No obvious backend cause found",
		},
		{
			name: "drops at parser level",
			counters: map[string]int64{
				"eventsDropped":         5,
				"clientFanOutDrops":     0,
				"fanOutDrops":           0,
				"controlModeReconnects": 0,
			},
			wantFindings: []string{"parser"},
			wantVerdict:  "5 total events dropped",
		},
		{
			name: "drops at client fan-out",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     3,
				"fanOutDrops":           0,
				"controlModeReconnects": 0,
			},
			wantFindings: []string{"client fan-out"},
			wantVerdict:  "3 total events dropped",
		},
		{
			name: "drops at tracker fan-out",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     0,
				"fanOutDrops":           7,
				"controlModeReconnects": 0,
			},
			wantFindings: []string{"tracker fan-out"},
			wantVerdict:  "7 total events dropped",
		},
		{
			name: "control mode reconnects",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     0,
				"fanOutDrops":           0,
				"controlModeReconnects": 2,
			},
			wantFindings: []string{"reconnect"},
		},
		{
			name: "output log near capacity",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     0,
				"fanOutDrops":           0,
				"controlModeReconnects": 0,
				"currentSeq":            48000,
				"logOldestSeq":          1000,
			},
			wantFindings: []string{"capacity"},
		},
		{
			name: "sync disabled annotation",
			counters: map[string]int64{
				"eventsDropped":         0,
				"clientFanOutDrops":     0,
				"fanOutDrops":           0,
				"controlModeReconnects": 0,
				"syncDisabled":          1,
			},
			wantFindings: []string{"No drops or anomalies detected", "sync is disabled"},
			wantVerdict:  "No obvious backend cause found",
		},
		{
			name: "multiple issues — drops + reconnects + near capacity",
			counters: map[string]int64{
				"eventsDropped":         10,
				"clientFanOutDrops":     5,
				"fanOutDrops":           3,
				"controlModeReconnects": 1,
				"currentSeq":            49000,
				"logOldestSeq":          1000,
			},
			wantFindings: []string{"parser", "client fan-out", "tracker fan-out", "reconnect", "capacity"},
			wantVerdict:  "18 total events dropped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, verdict := buildDiagnosticFindings(tt.counters)

			// Check that each expected substring appears in at least one finding
			for _, want := range tt.wantFindings {
				found := false
				for _, f := range findings {
					if containsSubstring(f, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected finding containing %q, got findings: %v", want, findings)
				}
			}

			// Check that unwanted substrings do NOT appear
			for _, notWant := range tt.notWantFindings {
				for _, f := range findings {
					if containsSubstring(f, notWant) {
						t.Errorf("unexpected finding containing %q: %q", notWant, f)
					}
				}
			}

			// Check verdict
			if tt.wantVerdict != "" && !containsSubstring(verdict, tt.wantVerdict) {
				t.Errorf("verdict = %q, want substring %q", verdict, tt.wantVerdict)
			}
		})
	}
}

func TestStatsMessage_SyncDisabled(t *testing.T) {
	msg := WSStatsMessage{
		Type:         "stats",
		SyncDisabled: true,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if decoded["syncDisabled"] != true {
		t.Errorf("syncDisabled = %v, want true", decoded["syncDisabled"])
	}
}

// containsSubstring does a case-insensitive substring check.
func containsSubstring(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestStatsMessage_InputLatency(t *testing.T) {
	msg := WSStatsMessage{
		Type: "stats",
		InputLatency: &LatencyPercentiles{
			DispatchP50:  0.5,
			DispatchP99:  1.2,
			SendKeysP50:  2.0,
			SendKeysP99:  5.0,
			EchoP50:      3.0,
			EchoP99:      8.0,
			FrameSendP50: 0.1,
			FrameSendP99: 0.3,
			SampleCount:  42,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	il, ok := decoded["inputLatency"].(map[string]interface{})
	if !ok {
		t.Fatal("inputLatency field missing or not an object")
	}
	if il["dispatchP50"].(float64) != 0.5 {
		t.Errorf("dispatchP50 = %v, want 0.5", il["dispatchP50"])
	}
	if il["sampleCount"].(float64) != 42 {
		t.Errorf("sampleCount = %v, want 42", il["sampleCount"])
	}
}

func TestStatsMessage_InputLatencyOmitted(t *testing.T) {
	msg := WSStatsMessage{
		Type: "stats",
		// InputLatency is nil — should be omitted from JSON
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if _, ok := decoded["inputLatency"]; ok {
		t.Error("inputLatency should be omitted when nil")
	}
}

func TestStatsMessage_InputLatencyContextFields(t *testing.T) {
	msg := WSStatsMessage{
		Type: "stats",
		InputLatency: &LatencyPercentiles{
			DispatchP50:      0.5,
			DispatchP99:      1.2,
			SendKeysP50:      2.0,
			SendKeysP99:      5.0,
			EchoP50:          3.0,
			EchoP99:          8.0,
			FrameSendP50:     0.1,
			FrameSendP99:     0.3,
			SampleCount:      42,
			OutputChDepthP50: 0.0,
			OutputChDepthP99: 3.0,
			EchoDataLenP50:   1.0,
			EchoDataLenP99:   512.0,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	il, ok := decoded["inputLatency"].(map[string]interface{})
	if !ok {
		t.Fatal("inputLatency field missing or not an object")
	}
	if il["outputChDepthP50"].(float64) != 0.0 {
		t.Errorf("outputChDepthP50 = %v, want 0.0", il["outputChDepthP50"])
	}
	if il["outputChDepthP99"].(float64) != 3.0 {
		t.Errorf("outputChDepthP99 = %v, want 3.0", il["outputChDepthP99"])
	}
	if il["echoDataLenP50"].(float64) != 1.0 {
		t.Errorf("echoDataLenP50 = %v, want 1.0", il["echoDataLenP50"])
	}
	if il["echoDataLenP99"].(float64) != 512.0 {
		t.Errorf("echoDataLenP99 = %v, want 512.0", il["echoDataLenP99"])
	}
}

func TestInputEchoSidebandFormat(t *testing.T) {
	// Verify the inputEcho sideband message format matches what the frontend expects
	sideband, err := json.Marshal(map[string]interface{}{
		"type":     "inputEcho",
		"serverMs": 5.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(sideband, &decoded)
	if decoded["type"] != "inputEcho" {
		t.Errorf("type = %v, want inputEcho", decoded["type"])
	}
	if decoded["serverMs"].(float64) != 5.2 {
		t.Errorf("serverMs = %v, want 5.2", decoded["serverMs"])
	}
}

// --- Async input sender + batching tests ---

// TestAsyncInputSender_DoesNotBlockSelectLoop verifies that the async sender
// goroutine pattern doesn't block the caller while SendInput is in flight.
func TestAsyncInputSender_DoesNotBlockSelectLoop(t *testing.T) {
	type inputBatch struct {
		data          string
		t1            time.Time
		t2            time.Time
		outputChDepth int
	}
	type inputResult struct {
		sendKeysDur   time.Duration
		t3            time.Time
		dispatch      time.Duration
		outputChDepth int
	}

	inputBatchCh := make(chan inputBatch, 10)
	inputDoneCh := make(chan inputResult, 10)

	// Simulate a slow SendInput (100ms)
	go func() {
		defer close(inputDoneCh)
		for batch := range inputBatchCh {
			t2 := time.Now()
			time.Sleep(100 * time.Millisecond) // simulate slow tmux
			t3 := time.Now()
			inputDoneCh <- inputResult{
				sendKeysDur:   t3.Sub(t2),
				t3:            t3,
				dispatch:      batch.t2.Sub(batch.t1),
				outputChDepth: batch.outputChDepth,
			}
		}
	}()

	// Send a batch — this should return immediately (not block for 100ms)
	start := time.Now()
	inputBatchCh <- inputBatch{
		data: "hello",
		t1:   time.Now(),
		t2:   time.Now(),
	}
	sendDuration := time.Since(start)

	if sendDuration > 10*time.Millisecond {
		t.Errorf("inputBatchCh send took %v, expected < 10ms (should be non-blocking)", sendDuration)
	}

	// The result should arrive after the simulated delay
	select {
	case result := <-inputDoneCh:
		if result.sendKeysDur < 90*time.Millisecond {
			t.Errorf("sendKeysDur = %v, expected >= 90ms", result.sendKeysDur)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inputDoneCh result")
	}

	close(inputBatchCh)
}

// TestPendingInputQueue_MultipleSamples verifies that the FIFO queue records
// one latency sample per keystroke, not just the last one (the singleton bug).
func TestPendingInputQueue_MultipleSamples(t *testing.T) {
	type pendingInputTiming struct {
		dispatch      time.Duration
		sendKeys      time.Duration
		t3            time.Time
		outputChDepth int
	}

	lc := NewLatencyCollector()
	var queue []pendingInputTiming

	// Simulate 3 keystrokes pushing timing into the queue
	for i := 1; i <= 3; i++ {
		queue = append(queue, pendingInputTiming{
			dispatch:      time.Duration(i) * time.Millisecond,
			sendKeys:      time.Duration(i*10) * time.Millisecond,
			t3:            time.Now(),
			outputChDepth: i,
		})
	}

	if len(queue) != 3 {
		t.Fatalf("queue length = %d, want 3", len(queue))
	}

	// Simulate 3 echo events popping from the queue
	for i := 0; i < 3; i++ {
		if len(queue) == 0 {
			t.Fatalf("queue unexpectedly empty at echo %d", i)
		}
		pending := queue[0]
		queue = queue[1:]
		lc.Add(LatencySample{
			Dispatch:      pending.dispatch,
			SendKeys:      pending.sendKeys,
			Echo:          time.Millisecond,
			FrameSend:     time.Millisecond,
			OutputChDepth: pending.outputChDepth,
		})
	}

	if len(queue) != 0 {
		t.Errorf("queue should be empty after all echoes, got %d", len(queue))
	}

	p := lc.Percentiles()
	if p == nil {
		t.Fatal("expected non-nil percentiles")
	}
	if p.SampleCount != 3 {
		t.Errorf("SampleCount = %d, want 3 (all keystrokes should be recorded)", p.SampleCount)
	}
}

// TestKeystrokeBatching_DrainCoalesces verifies that the non-blocking drain
// pattern coalesces multiple queued input messages into a single combined string.
func TestKeystrokeBatching_DrainCoalesces(t *testing.T) {
	// Pre-fill a channel with 3 input messages and 1 resize message
	controlChan := make(chan WSMessage, 10)
	controlChan <- WSMessage{Type: "input", Data: "b"}
	controlChan <- WSMessage{Type: "input", Data: "c"}
	controlChan <- WSMessage{Type: "resize", Data: `{"cols":120,"rows":40}`}
	controlChan <- WSMessage{Type: "input", Data: "d"}

	// Simulate the first message already received from the select case
	combined := "a" // first keystroke from case msg := <-controlChan

	// Non-blocking drain — same pattern as the production code
	var resizeCount int
drain:
	for {
		select {
		case extra, ok := <-controlChan:
			if !ok {
				t.Fatal("channel unexpectedly closed")
			}
			if extra.Type == "input" {
				if !isTerminalResponse(extra.Data) {
					combined += extra.Data
				}
			} else if extra.Type == "resize" {
				resizeCount++
			}
		default:
			break drain
		}
	}

	if combined != "abcd" {
		t.Errorf("combined = %q, want %q", combined, "abcd")
	}
	if resizeCount != 1 {
		t.Errorf("resize messages handled = %d, want 1", resizeCount)
	}
}

// TestKeystrokeBatching_TerminalResponsesFiltered verifies that terminal
// query responses mixed in with keystrokes are filtered during drain.
func TestKeystrokeBatching_TerminalResponsesFiltered(t *testing.T) {
	controlChan := make(chan WSMessage, 10)
	controlChan <- WSMessage{Type: "input", Data: "\x1b[?1;2c"} // DA1 response — should be filtered
	controlChan <- WSMessage{Type: "input", Data: "x"}

	combined := "a"

drain:
	for {
		select {
		case extra := <-controlChan:
			if extra.Type == "input" {
				if !isTerminalResponse(extra.Data) {
					combined += extra.Data
				}
			}
		default:
			break drain
		}
	}

	if combined != "ax" {
		t.Errorf("combined = %q, want %q (terminal response should be filtered)", combined, "ax")
	}
}

// TestAsyncInputSender_TimingFlowsToQueue verifies that the async sender
// goroutine correctly produces timing results that flow into the pending queue.
func TestAsyncInputSender_TimingFlowsToQueue(t *testing.T) {
	type pendingInputTiming struct {
		dispatch      time.Duration
		sendKeys      time.Duration
		t3            time.Time
		outputChDepth int
	}
	type inputBatch struct {
		data          string
		t1            time.Time
		t2            time.Time
		outputChDepth int
	}
	type inputResult struct {
		sendKeysDur   time.Duration
		t3            time.Time
		dispatch      time.Duration
		outputChDepth int
	}

	inputBatchCh := make(chan inputBatch, 10)
	inputDoneCh := make(chan inputResult, 10)

	// Minimal sender — no delay
	go func() {
		defer close(inputDoneCh)
		for batch := range inputBatchCh {
			t3 := time.Now()
			inputDoneCh <- inputResult{
				sendKeysDur:   time.Millisecond,
				t3:            t3,
				dispatch:      batch.t2.Sub(batch.t1),
				outputChDepth: batch.outputChDepth,
			}
		}
	}()

	// Send 3 batches
	for i := 0; i < 3; i++ {
		t1 := time.Now()
		inputBatchCh <- inputBatch{
			data:          string(rune('a' + i)),
			t1:            t1,
			t2:            time.Now(),
			outputChDepth: i,
		}
	}

	// Collect results into queue
	var queue []pendingInputTiming
	for i := 0; i < 3; i++ {
		select {
		case result := <-inputDoneCh:
			queue = append(queue, pendingInputTiming{
				dispatch:      result.dispatch,
				sendKeys:      result.sendKeysDur,
				t3:            result.t3,
				outputChDepth: result.outputChDepth,
			})
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for result %d", i)
		}
	}

	if len(queue) != 3 {
		t.Errorf("queue length = %d, want 3", len(queue))
	}

	// Verify ordering — outputChDepth should be 0, 1, 2
	for i, p := range queue {
		if p.outputChDepth != i {
			t.Errorf("queue[%d].outputChDepth = %d, want %d", i, p.outputChDepth, i)
		}
	}

	close(inputBatchCh)
}

// makeWSRequest creates an HTTP request with chi route context for a WebSocket endpoint.
func makeWSRequest(t *testing.T, path, sessionID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestHandleTerminalWebSocket_MissingSessionID(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := makeWSRequest(t, "/ws/terminal/", "")
	rr := httptest.NewRecorder()
	server.handleTerminalWebSocket(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTerminalWebSocket_SessionNotRunning(t *testing.T) {
	server, _, st := newTestServer(t)

	// Add a session that is stopped (not running)
	st.AddSession(state.Session{
		ID:          "sess-ws-1",
		TmuxSession: "schmux-nonexistent-ws1",
		Status:      "stopped",
	})

	req := makeWSRequest(t, "/ws/terminal/sess-ws-1", "sess-ws-1")
	rr := httptest.NewRecorder()
	server.handleTerminalWebSocket(rr, req)

	// Session exists in state but IsRunning returns false → 410 Gone
	if rr.Code != http.StatusGone {
		t.Errorf("expected 410 (session not running), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTerminalWebSocket_SessionNotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	// Session ID doesn't exist in state at all
	req := makeWSRequest(t, "/ws/terminal/nonexistent-sess", "nonexistent-sess")
	rr := httptest.NewRecorder()
	server.handleTerminalWebSocket(rr, req)

	// IsRunning returns false for nonexistent → 410 Gone
	if rr.Code != http.StatusGone {
		t.Errorf("expected 410, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWaitForTrackerAttach_TimesOut(t *testing.T) {
	// Use the session manager's GetTracker to get a real tracker for a nonexistent
	// tmux session — it won't be attached, so waitForTrackerAttach should time out.
	srv, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "sess-wait", TmuxSession: "nonexistent-tmux-wait"})

	tracker, err := srv.session.GetTracker("sess-wait")
	if err != nil {
		t.Fatalf("GetTracker: %v", err)
	}
	defer srv.session.Stop()

	start := time.Now()
	waitForTrackerAttach(tracker, 50*time.Millisecond)
	elapsed := time.Since(start)

	// Should exit after ~50ms timeout since tracker can't attach
	if elapsed > 500*time.Millisecond {
		t.Errorf("waitForTrackerAttach took too long: %v (expected ~50ms timeout)", elapsed)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("waitForTrackerAttach returned too quickly: %v (expected ~50ms)", elapsed)
	}
}
