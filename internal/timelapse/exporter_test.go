package timelapse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createSyntheticRecording(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test-recording.cast")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	// Asciicast v2 header
	fmt.Fprintln(f, `{"version":2,"width":40,"height":10,"timestamp":1711875300,"env":{"TERM":"xterm-256color"}}`)

	// Burst of scrolling output with distinct lines
	for i := 0; i < 15; i++ {
		data := fmt.Sprintf("output line %03d with unique content here\r\n", i)
		escaped := jsonEscapeBytes([]byte(data))
		fmt.Fprintf(f, "[%.6f,\"o\",%s]\n", float64(i)*0.1, escaped)
	}

	// Idle gap — spinner-like single-char updates (no scroll)
	for i := 0; i < 20; i++ {
		data := fmt.Sprintf("\033[5;1H%c", "abcdef"[i%6])
		escaped := jsonEscapeBytes([]byte(data))
		fmt.Fprintf(f, "[%.6f,\"o\",%s]\n", 1.5+float64(i)*0.15, escaped)
	}

	// More scrolling output after idle
	for i := 0; i < 15; i++ {
		data := fmt.Sprintf("after idle line %03d unique text here\r\n", i)
		escaped := jsonEscapeBytes([]byte(data))
		fmt.Fprintf(f, "[%.6f,\"o\",%s]\n", 4.5+float64(i)*0.1, escaped)
	}

	f.Close()
	return path
}

func TestExporter_BasicExport(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.timelapse.cast")

	var lastProgress float64
	exp := NewExporter(recordingPath, outputPath, func(pct float64) {
		lastProgress = pct
	})

	if err := exp.Export(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// Valid asciicast v2
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("invalid header: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("version = %v, want 2", header["version"])
	}

	if lastProgress < 0.9 {
		t.Errorf("lastProgress = %f, want near 1.0", lastProgress)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestExporter_AllEventsPreserved(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.timelapse.cast")

	exp := NewExporter(recordingPath, outputPath, nil)
	if err := exp.Export(); err != nil {
		t.Fatal(err)
	}

	countEvents := func(path string) int {
		n := 0
		f, _ := os.Open(path)
		defer f.Close()
		ReadCastEvents(f, func(rec Record) bool {
			if rec.Type == RecordOutput {
				n++
			}
			return true
		})
		return n
	}

	orig := countEvents(recordingPath)
	comp := countEvents(outputPath)
	if comp != orig {
		t.Errorf("compressed has %d events, original has %d — all should be preserved", comp, orig)
	}
}

func TestExporter_CompressedDuration(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.timelapse.cast")

	if err := NewExporter(recordingPath, outputPath, nil).Export(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	lastLine := lines[len(lines)-1]
	var ev [3]json.RawMessage
	json.Unmarshal([]byte(lastLine), &ev)
	var compDuration float64
	json.Unmarshal(ev[0], &compDuration)

	// Original spans ~6s, compressed should be shorter
	if compDuration >= 6.0 {
		t.Errorf("compressed duration = %.1fs, should be < 6.0s", compDuration)
	}
	t.Logf("compressed: %.2fs (original: ~6.0s)", compDuration)
}

func TestExporter_PreservesContent(t *testing.T) {
	dir := t.TempDir()
	recordingPath := createSyntheticRecording(t, dir)
	outputPath := filepath.Join(dir, "output.timelapse.cast")

	if err := NewExporter(recordingPath, outputPath, nil).Export(); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(outputPath)
	defer f.Close()
	var all strings.Builder
	ReadCastEvents(f, func(rec Record) bool {
		if rec.Type == RecordOutput {
			all.WriteString(rec.D)
		}
		return true
	})

	if !strings.Contains(all.String(), "output line") {
		t.Error("should contain 'output line'")
	}
	if !strings.Contains(all.String(), "after idle") {
		t.Error("should contain 'after idle'")
	}
}
