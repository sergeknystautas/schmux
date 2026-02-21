package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticCapture_FullFlow(t *testing.T) {
	dir := t.TempDir()
	diag := &DiagnosticCapture{
		Timestamp:   time.Now(),
		SessionID:   "integration-test",
		Cols:        80,
		Rows:        24,
		Counters:    map[string]int64{"eventsDelivered": 500, "eventsDropped": 2, "bytesDelivered": 100000, "controlModeReconnects": 1},
		TmuxScreen:  "$ ls\nfile1.txt\nfile2.txt\n",
		RingBuffer:  []byte("\033[1m$ ls\033[0m\nfile1.txt\nfile2.txt\n"),
		Findings:    []string{"2 events dropped"},
		Verdict:     "Events were dropped due to channel backpressure.",
		DiffSummary: "1 row differs",
	}
	if err := diag.WriteToDir(dir); err != nil {
		t.Fatal(err)
	}
	// Verify meta.json content
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta map[string]interface{}
	json.Unmarshal(data, &meta)
	counters := meta["counters"].(map[string]interface{})
	if int(counters["eventsDropped"].(float64)) != 2 {
		t.Errorf("eventsDropped = %v, want 2", counters["eventsDropped"])
	}
	// Verify raw files are not base64
	tmuxData, _ := os.ReadFile(filepath.Join(dir, "screen-tmux.txt"))
	if !strings.Contains(string(tmuxData), "$ ls") {
		t.Error("screen-tmux.txt should contain raw text")
	}
	rbData, _ := os.ReadFile(filepath.Join(dir, "ringbuffer-backend.txt"))
	if !strings.Contains(string(rbData), "\033[1m") {
		t.Error("ringbuffer-backend.txt should contain raw ANSI sequences")
	}
}
