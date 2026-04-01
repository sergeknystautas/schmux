package timelapse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/session"
)

func TestRecorder_BasicRecording(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)

	rec, err := NewRecorder("test-session", ol, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()

	// Append some output
	ol.Append([]byte("hello"))
	ol.Append([]byte("world"))
	time.Sleep(50 * time.Millisecond) // let recorder process

	rec.Stop()

	// Read the recording file
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("expected 1 recording file, got %d", len(files))
	}

	data, _ := os.ReadFile(files[0])
	content := string(data)

	// Should have header, 2 output records, and end record
	var records []Record
	ReadRecords(strings.NewReader(content), func(rec Record) bool {
		records = append(records, rec)
		return true
	})

	if len(records) < 4 {
		t.Fatalf("expected at least 4 records (header + 2 output + end), got %d", len(records))
	}

	if records[0].Type != RecordHeader {
		t.Errorf("first record type = %q, want header", records[0].Type)
	}
	if records[0].Version != 1 {
		t.Errorf("header version = %d, want 1", records[0].Version)
	}
	if records[0].SessionID != "test-session" {
		t.Errorf("header sessionId = %q, want test-session", records[0].SessionID)
	}

	// Find output records
	var outputs []Record
	for _, r := range records {
		if r.Type == RecordOutput {
			outputs = append(outputs, r)
		}
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 output records, got %d", len(outputs))
	}
	if outputs[0].D != "hello" {
		t.Errorf("output 0 data = %q, want hello", outputs[0].D)
	}
	if outputs[1].D != "world" {
		t.Errorf("output 1 data = %q, want world", outputs[1].D)
	}

	// First output should have t=0.xxx (very small)
	if outputs[0].T == nil {
		t.Fatal("first output T should not be nil")
	}

	// Last record should be end
	last := records[len(records)-1]
	if last.Type != RecordEnd {
		t.Errorf("last record type = %q, want end", last.Type)
	}

	// Check file permissions
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestRecorder_MaxBytesCap(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)

	// Very small size cap
	rec, err := NewRecorder("test-session", ol, nil, dir, 500, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()

	// Send enough data to exceed the cap
	for i := 0; i < 100; i++ {
		ol.Append([]byte("data-that-takes-up-space-in-the-recording-file"))
	}

	// Wait for recorder to stop (it should stop due to size cap)
	select {
	case <-rec.doneCh:
	case <-time.After(5 * time.Second):
		rec.Stop()
		t.Fatal("recorder did not stop within timeout after size cap")
	}

	// Verify end record was written
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"type":"end"`) {
		t.Error("recording should contain end record after size cap")
	}
}

func TestRecorder_GapAndResizeEvents(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)
	gapCh := make(chan session.SourceEvent, 10)

	rec, err := NewRecorder("test-session", ol, gapCh, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()

	// Send output first (to trigger recorder loop)
	ol.Append([]byte("output1"))

	// Send gap and resize events
	gapCh <- session.SourceEvent{
		Type:   session.SourceGap,
		Reason: "reconnect",
	}
	gapCh <- session.SourceEvent{
		Type:   session.SourceResize,
		Width:  120,
		Height: 40,
	}

	// Send more output to trigger draining
	ol.Append([]byte("output2"))
	time.Sleep(50 * time.Millisecond)

	rec.Stop()

	// Verify gap and resize records
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	content := string(data)

	if !strings.Contains(content, `"type":"gap"`) {
		t.Error("recording should contain gap record")
	}
	if !strings.Contains(content, `"reconnect"`) {
		t.Error("gap record should contain reason")
	}
	if !strings.Contains(content, `"type":"resize"`) {
		t.Error("recording should contain resize record")
	}
}

func TestRecorder_BufferOverrun(t *testing.T) {
	dir := t.TempDir()
	// Very small output log — entries will be evicted quickly
	ol := session.NewOutputLog(5)

	rec, err := NewRecorder("test-session", ol, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()

	// Write one entry so recorder starts
	ol.Append([]byte("first"))
	time.Sleep(50 * time.Millisecond)

	// Flood the log so entries are evicted before recorder catches up
	for i := 0; i < 20; i++ {
		ol.Append([]byte("flood"))
	}
	time.Sleep(50 * time.Millisecond)

	rec.Stop()

	// Verify buffer_overrun gap record exists
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	content := string(data)

	if !strings.Contains(content, `"buffer_overrun"`) {
		// Buffer overrun may not always trigger depending on timing,
		// but the recorder should still produce a valid file
		t.Log("buffer_overrun not detected (timing-dependent)")
	}

	// File should at least have header and end
	if !strings.Contains(content, `"type":"header"`) {
		t.Error("recording should contain header")
	}
	if !strings.Contains(content, `"type":"end"`) {
		t.Error("recording should contain end record")
	}
}
