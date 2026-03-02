package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteDiagnosticDir(t *testing.T) {
	dir := t.TempDir()
	diag := &DiagnosticCapture{
		Timestamp:         time.Now(),
		SessionID:         "test-session",
		Cols:              120,
		Rows:              40,
		Counters:          map[string]int64{"eventsDelivered": 100, "eventsDropped": 0},
		TmuxScreen:        "$ hello\n$ world\n",
		RingBuffer:        []byte("\033[1mhello\033[0m\n"),
		Findings:          []string{"No drops detected"},
		Verdict:           "No obvious cause found.",
		DiffSummary:       "0 rows differ",
		CursorTmuxX:       42,
		CursorTmuxY:       10,
		CursorTmuxVisible: true,
	}
	err := diag.WriteToDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Verify files exist
	for _, name := range []string{"meta.json", "ringbuffer-backend.txt", "screen-tmux.txt"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("missing file: %s", name)
		}
	}
	// Verify meta.json is valid JSON
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Errorf("meta.json is not valid JSON: %v", err)
	}
	if meta["sessionId"] != "test-session" {
		t.Errorf("sessionId = %v, want test-session", meta["sessionId"])
	}
	// Verify cursorTmux is present and correct
	cursorRaw, ok := meta["cursorTmux"]
	if !ok {
		t.Fatal("meta.json missing cursorTmux field")
	}
	cursorMap, ok := cursorRaw.(map[string]interface{})
	if !ok {
		t.Fatal("cursorTmux is not an object")
	}
	if cursorMap["x"] != float64(42) {
		t.Errorf("cursorTmux.x = %v, want 42", cursorMap["x"])
	}
	if cursorMap["y"] != float64(10) {
		t.Errorf("cursorTmux.y = %v, want 10", cursorMap["y"])
	}
	if cursorMap["visible"] != true {
		t.Errorf("cursorTmux.visible = %v, want true", cursorMap["visible"])
	}
	// Verify ring buffer is raw text, not base64
	rbData, _ := os.ReadFile(filepath.Join(dir, "ringbuffer-backend.txt"))
	if string(rbData) != "\033[1mhello\033[0m\n" {
		t.Errorf("ringbuffer-backend.txt content mismatch")
	}
}

func TestWriteDiagnosticDir_CursorError(t *testing.T) {
	dir := t.TempDir()
	diag := &DiagnosticCapture{
		Timestamp:     time.Now(),
		SessionID:     "test-session-no-cursor",
		Cols:          80,
		Rows:          24,
		Counters:      map[string]int64{},
		TmuxScreen:    "$ test\n",
		RingBuffer:    []byte("test\n"),
		CursorTmuxErr: "timeout getting cursor state",
	}
	err := diag.WriteToDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("meta.json is not valid JSON: %v", err)
	}
	// cursorTmux should be omitted when there's an error
	if _, ok := meta["cursorTmux"]; ok {
		t.Error("cursorTmux should be omitted when CursorTmuxErr is set")
	}
}
