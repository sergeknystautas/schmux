package timelapse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createSyntheticRecording(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test-recording.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	// Header
	WriteRecord(f, Record{
		Type:        RecordHeader,
		Version:     1,
		RecordingID: "test-export",
		SessionID:   "s1",
		Width:       40,
		Height:      10,
		StartTime:   "2026-03-31T12:00:00Z",
	})

	// Burst of output with distinct lines (triggers scroll detection)
	for i := 0; i < 15; i++ {
		WriteRecord(f, Record{
			Type: RecordOutput,
			T:    floatPtr(float64(i) * 0.1),
			Seq:  uint64(i),
			D:    fmt.Sprintf("output line %03d with unique content here\r\n", i),
		})
	}

	// Idle gap (3 seconds with no output)
	// ... nothing happens from t=1.5 to t=4.0

	// More distinct output after idle
	for i := 0; i < 15; i++ {
		WriteRecord(f, Record{
			Type: RecordOutput,
			T:    floatPtr(4.0 + float64(i)*0.1),
			Seq:  uint64(15 + i),
			D:    fmt.Sprintf("after idle line %03d unique text here\r\n", i),
		})
	}

	// End
	WriteRecord(f, Record{
		Type: RecordEnd,
		T:    floatPtr(4.5),
	})

	f.Close()
	return path
}

func TestExporter_BasicExport(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.cast")

	var lastProgress float64
	exp := NewExporter(recordingPath, outputPath, func(pct float64) {
		lastProgress = pct
	})

	err := exp.Export()
	if err != nil {
		t.Fatal(err)
	}

	// Verify output file exists
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// Verify it's valid asciicast v2 (NDJSON with header)
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (header + events), got %d", len(lines))
	}

	// Verify header
	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header JSON: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("header version = %v, want 2", header["version"])
	}
	if header["width"].(float64) != 40 {
		t.Errorf("header width = %v, want 40", header["width"])
	}

	// Verify events are valid
	for i, line := range lines[1:] {
		var event [3]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("invalid event JSON at line %d: %v", i+1, err)
		}
	}

	// Verify progress was reported
	if lastProgress < 0.9 {
		t.Errorf("lastProgress = %f, expected near 1.0", lastProgress)
	}

	// Verify file permissions
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestExporter_CompressedDuration(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.cast")

	exp := NewExporter(recordingPath, outputPath, nil)
	err := exp.Export()
	if err != nil {
		t.Fatal(err)
	}

	// Read the .cast header to check duration
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	var header struct {
		Duration float64 `json:"duration"`
	}
	json.Unmarshal([]byte(lines[0]), &header)

	// The compressed duration should be shorter than the original (4.5s)
	// because the idle gap (1.0s to 4.0s) is compressed to 300ms
	if header.Duration >= 4.5 {
		t.Errorf("compressed duration = %f, should be less than original (4.5)", header.Duration)
	}
	t.Logf("compressed duration: %.2fs (original: 4.5s)", header.Duration)
}

func TestExporter_PreservesContent(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.cast")

	exp := NewExporter(recordingPath, outputPath, nil)
	if err := exp.Export(); err != nil {
		t.Fatal(err)
	}

	// Read all event data from the .cast file
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	var allData bytes.Buffer
	for _, line := range lines[1:] {
		var event [3]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		allData.WriteString(event[2].(string))
	}

	content := allData.String()
	if !strings.Contains(content, "output line") {
		t.Error("exported content should contain 'output line'")
	}
}
