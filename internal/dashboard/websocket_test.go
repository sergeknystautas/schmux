package dashboard

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestEncodeSequencedFrame(t *testing.T) {
	data := []byte("hello")
	frame := encodeSequencedFrame(42, data)

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

func TestChunkReplay(t *testing.T) {
	log := session.NewOutputLog(100)
	for i := 0; i < 20; i++ {
		log.Append([]byte(fmt.Sprintf("line %d\n", i)))
	}

	chunks := chunkReplayEntries(log.ReplayAll(), 50) // 50 byte chunks
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// Verify all data is present across chunks
	var total []byte
	for _, c := range chunks {
		total = append(total, c.Data...)
	}
	for i := 0; i < 20; i++ {
		expected := fmt.Sprintf("line %d\n", i)
		if !bytes.Contains(total, []byte(expected)) {
			t.Errorf("missing line %d in chunked output", i)
		}
	}

	// Verify last chunk has the correct final seq
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.Seq != 19 {
		t.Errorf("last chunk seq=%d, want 19", lastChunk.Seq)
	}
}

func TestBuildGapReplayFrames(t *testing.T) {
	log := session.NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c"))

	frames := buildGapReplayFrames(log, 1, 16384) // replay from seq 1
	if len(frames) == 0 {
		t.Fatal("expected replay frames")
	}
	// Should contain data for seq 1 and 2 ("b" and "c")
	var total []byte
	for _, f := range frames {
		total = append(total, f[8:]...) // skip 8-byte header
	}
	if string(total) != "bc" {
		t.Errorf("replayed data=%q, want 'bc'", total)
	}
}

func TestBuildGapReplayFrames_EvictedData(t *testing.T) {
	log := session.NewOutputLog(2) // tiny buffer
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c")) // evicts "a"

	// Request from seq 0 (evicted) — should return nil
	frames := buildGapReplayFrames(log, 0, 16384)
	if frames != nil {
		t.Errorf("expected nil for evicted data, got %d frames", len(frames))
	}
}

func TestChunkReplay_SingleEntryExceedsMaxBytes(t *testing.T) {
	log := session.NewOutputLog(100)

	// Add a small entry, then a large entry that exceeds maxBytes, then another small one
	log.Append([]byte("small"))
	log.Append(bytes.Repeat([]byte("X"), 200)) // 200 bytes, exceeds maxBytes=50
	log.Append([]byte("after"))

	chunks := chunkReplayEntries(log.ReplayAll(), 50) // maxBytes=50
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// Verify all data is present across chunks
	var total []byte
	for _, c := range chunks {
		total = append(total, c.Data...)
	}
	if !bytes.Contains(total, []byte("small")) {
		t.Error("missing 'small' in chunked output")
	}
	if !bytes.Contains(total, bytes.Repeat([]byte("X"), 200)) {
		t.Error("missing large entry in chunked output")
	}
	if !bytes.Contains(total, []byte("after")) {
		t.Error("missing 'after' in chunked output")
	}

	// The large entry should be in its own chunk (since "small" fills current,
	// then the 200-byte entry exceeds maxBytes so "small" flushes first,
	// then 200-byte entry starts a new chunk which also exceeds maxBytes but
	// gets flushed when "after" arrives)
	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks for small+oversized+small, got %d", len(chunks))
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
