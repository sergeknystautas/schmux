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

func TestRecorder_HeaderUsesProvidedDimensions(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)

	// Use non-default dimensions to verify they're written to the header
	rec, err := NewRecorder("test-dims", ol, nil, dir, 0, 200, 50)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()
	ol.Append([]byte("hello"))
	time.Sleep(50 * time.Millisecond)
	rec.Stop()

	data, _ := os.ReadFile(filepath.Join(dir, "test-dims.cast"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least a header line")
	}

	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header JSON: %v", err)
	}
	if w := int(header["width"].(float64)); w != 200 {
		t.Errorf("header width = %d, want 200", w)
	}
	if h := int(header["height"].(float64)); h != 50 {
		t.Errorf("header height = %d, want 50", h)
	}
}

func TestRecorder_FileNamedBySessionID(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)

	rec, err := NewRecorder("my-session-abc", ol, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	go rec.Run()
	ol.Append([]byte("hello"))
	time.Sleep(50 * time.Millisecond)
	rec.Stop()

	// File should be named exactly <sessionID>.cast — no timestamp suffix.
	expected := filepath.Join(dir, "my-session-abc.cast")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("expected file %s to exist", expected)
	}
	if rec.RecordingID() != "my-session-abc" {
		t.Errorf("RecordingID = %q, want %q", rec.RecordingID(), "my-session-abc")
	}
}

func TestRecorder_ResumesExistingRecording(t *testing.T) {
	dir := t.TempDir()
	ol := session.NewOutputLog(1000)

	// Create initial recording.
	rec1, err := NewRecorder("sess-resume", ol, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	go rec1.Run()
	ol.Append([]byte("part1"))
	time.Sleep(50 * time.Millisecond)
	rec1.Stop()

	// Read initial file content.
	castFile := filepath.Join(dir, "sess-resume.cast")
	data1, _ := os.ReadFile(castFile)
	lines1 := strings.Split(strings.TrimSpace(string(data1)), "\n")

	// Create a new recorder for the same session — should resume.
	ol2 := session.NewOutputLog(1000)
	rec2, err := NewRecorder("sess-resume", ol2, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.RecordingID() != "sess-resume" {
		t.Errorf("resumed RecordingID = %q, want %q", rec2.RecordingID(), "sess-resume")
	}

	go rec2.Run()
	ol2.Append([]byte("part2"))
	time.Sleep(50 * time.Millisecond)
	rec2.Stop()

	// Should still be one file.
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file after resume, got %d", len(files))
	}

	// File should have exactly one header and events from both recordings.
	data2, _ := os.ReadFile(castFile)
	lines2 := strings.Split(strings.TrimSpace(string(data2)), "\n")

	// The resumed file should have more lines but still only one header.
	if len(lines2) <= len(lines1) {
		t.Fatalf("resumed file should have more lines: before=%d, after=%d", len(lines1), len(lines2))
	}

	// Count headers — should be exactly one.
	headerCount := 0
	for _, line := range lines2 {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(line), &obj) == nil {
			if _, ok := obj["version"]; ok {
				headerCount++
			}
		}
	}
	if headerCount != 1 {
		t.Errorf("expected 1 header in resumed file, got %d", headerCount)
	}
}

func TestRecorder_ResumesLegacyTimestampedFile(t *testing.T) {
	dir := t.TempDir()

	// Create a legacy-format recording: <sessionID>-<timestamp>.cast
	startTime := time.Now().Add(-1 * time.Hour)
	createTestRecording(t, dir, "sess-legacy-1700000000", "sess-legacy", startTime, 5.0)

	// NewRecorder should find and resume the legacy file.
	ol := session.NewOutputLog(1000)
	rec, err := NewRecorder("sess-legacy", ol, nil, dir, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}

	// Should have resumed the legacy file, not created a new one.
	if rec.RecordingID() != "sess-legacy-1700000000" {
		t.Errorf("RecordingID = %q, want legacy %q", rec.RecordingID(), "sess-legacy-1700000000")
	}

	go rec.Run()
	ol.Append([]byte("new data"))
	time.Sleep(50 * time.Millisecond)
	rec.Stop()

	// Should still be one .cast file (the legacy one, now with appended data).
	files, _ := filepath.Glob(filepath.Join(dir, "*.cast"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
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
