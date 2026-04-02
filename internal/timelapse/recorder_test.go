package timelapse

import (
	"encoding/json"
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

	// Read the recording file (.cast format)
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	if len(files) != 1 {
		t.Fatalf("expected 1 recording file, got %d", len(files))
	}

	data, _ := os.ReadFile(files[0])
	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 events), got %d", len(lines))
	}

	// First line should be asciicast v2 header
	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header JSON: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("header version = %v, want 2", header["version"])
	}

	// Parse events using ReadCastEvents
	var records []Record
	ReadCastEvents(strings.NewReader(content), func(rec Record) bool {
		records = append(records, rec)
		return true
	})

	if len(records) < 3 {
		t.Fatalf("expected at least 3 records (header + 2 output), got %d", len(records))
	}

	if records[0].Type != RecordHeader {
		t.Errorf("first record type = %q, want header", records[0].Type)
	}

	// Find output records
	var outputs []Record
	for _, r := range records {
		if r.Type == RecordOutput {
			outputs = append(outputs, r)
		}
	}
	if len(outputs) < 2 {
		t.Fatalf("expected at least 2 output records, got %d", len(outputs))
	}
	if outputs[0].D != "hello" {
		t.Errorf("output 0 data = %q, want hello", outputs[0].D)
	}
	if outputs[1].D != "world" {
		t.Errorf("output 1 data = %q, want world", outputs[1].D)
	}

	// First output should have t close to 0
	if outputs[0].T == nil {
		t.Fatal("first output T should not be nil")
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

	// Verify recording file exists
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	data, _ := os.ReadFile(files[0])
	content := string(data)

	// Should have an asciicast header
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 1 {
		t.Fatal("recording should have at least a header line")
	}
	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Error("header version should be 2")
	}
}

func TestRecorder_ResizeEvents(t *testing.T) {
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

	// Send resize event
	gapCh <- session.SourceEvent{
		Type:   session.SourceResize,
		Width:  120,
		Height: 40,
	}

	// Send more output to trigger draining
	ol.Append([]byte("output2"))
	time.Sleep(50 * time.Millisecond)

	rec.Stop()

	// Verify resize event in the .cast file
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	data, _ := os.ReadFile(files[0])
	content := string(data)

	// Should contain a resize event line like [t,"r","120x40"]
	if !strings.Contains(content, `"r"`) {
		t.Error("recording should contain resize event")
	}
	if !strings.Contains(content, `"120x40"`) {
		t.Error("resize event should contain dimensions")
	}

	// Parse and verify resize record
	var records []Record
	ReadCastEvents(strings.NewReader(content), func(rec Record) bool {
		records = append(records, rec)
		return true
	})
	var foundResize bool
	for _, r := range records {
		if r.Type == RecordResize && r.Width == 120 && r.Height == 40 {
			foundResize = true
		}
	}
	if !foundResize {
		t.Error("should have a resize record with 120x40")
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

	// Verify recording file exists and has header
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	data, _ := os.ReadFile(files[0])
	content := string(data)

	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 1 {
		t.Fatal("recording should have at least a header")
	}
	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Error("header version should be 2")
	}
}
