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
		Timestamp:   time.Now(),
		SessionID:   "test-session",
		Cols:        120,
		Rows:        40,
		Counters:    map[string]int64{"eventsDelivered": 100, "eventsDropped": 0},
		TmuxScreen:  "$ hello\n$ world\n",
		RingBuffer:  []byte("\033[1mhello\033[0m\n"),
		Findings:    []string{"No drops detected"},
		Verdict:     "No obvious cause found.",
		DiffSummary: "0 rows differ",
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
	// Verify ring buffer is raw text, not base64
	rbData, _ := os.ReadFile(filepath.Join(dir, "ringbuffer-backend.txt"))
	if string(rbData) != "\033[1mhello\033[0m\n" {
		t.Errorf("ringbuffer-backend.txt content mismatch")
	}
}
