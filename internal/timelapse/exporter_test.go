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

func TestExporter_PreservesResizeEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "with-resize.cast")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	// Header at 80x24
	fmt.Fprintln(f, `{"version":2,"width":80,"height":24,"timestamp":1711875300,"env":{"TERM":"xterm-256color"}}`)
	// Some output
	fmt.Fprintln(f, `[0.100000,"o","hello\r\n"]`)
	// Resize event
	fmt.Fprintln(f, `[0.500000,"r","162x90"]`)
	// More output after resize
	fmt.Fprintln(f, `[1.000000,"o","world\r\n"]`)
	f.Close()

	outputPath := filepath.Join(dir, "output.timelapse.cast")
	if err := NewExporter(path, outputPath, nil).Export(); err != nil {
		t.Fatal(err)
	}

	// Read exported events and verify resize is preserved as "r" type
	outF, _ := os.Open(outputPath)
	defer outF.Close()
	var foundResize bool
	ReadCastEvents(outF, func(rec Record) bool {
		if rec.Type == RecordResize && rec.Width == 162 && rec.Height == 90 {
			foundResize = true
		}
		return true
	})
	if !foundResize {
		t.Error("exported timelapse should contain resize event as 'r' type with 162x90")
	}
}

func TestExporter_CosmeticEventsCollapsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cosmetic.cast")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	// Header at 40x10
	fmt.Fprintln(f, `{"version":2,"width":40,"height":10,"timestamp":1711875300,"env":{"TERM":"xterm-256color"}}`)

	// Initial content to establish screen state
	fmt.Fprintln(f, `[0.100000,"o","First line of content\r\n"]`)

	// Simulate Claude Code spinner: many events that each change only 1 character
	// Using cursor positioning to overwrite a single cell (col 1, row 3)
	spinners := []string{"a", "b", "c", "d", "e", "f", "a", "b", "c", "d"}
	for i, ch := range spinners {
		data := fmt.Sprintf("\033[3;1H%s", ch) // Move to row 3, col 1, write char
		escaped := jsonEscapeBytes([]byte(data))
		fmt.Fprintf(f, "[%.6f,\"o\",%s]\n", 1.0+float64(i)*0.5, escaped)
	}

	// More real content after the spinner
	for i := 0; i < 10; i++ {
		data := fmt.Sprintf("real output line %03d with text\r\n", i)
		escaped := jsonEscapeBytes([]byte(data))
		fmt.Fprintf(f, "[%.6f,\"o\",%s]\n", 6.0+float64(i)*0.1, escaped)
	}

	f.Close()

	outputPath := filepath.Join(dir, "output.timelapse.cast")
	if err := NewExporter(path, outputPath, nil).Export(); err != nil {
		t.Fatal(err)
	}

	// Read compressed timestamps
	outF, _ := os.Open(outputPath)
	defer outF.Close()
	var timestamps []float64
	ReadCastEvents(outF, func(rec Record) bool {
		if rec.Type == RecordOutput && rec.T != nil {
			timestamps = append(timestamps, *rec.T)
		}
		return true
	})

	// The 10 spinner events should have zero timestamp advance between them
	// (they're cosmetic — each changes only 1 cell).
	// The first spinner event is at index 1 (after "First line of content").
	// Events at indices 1..10 are spinner, 11..20 are real content.
	if len(timestamps) < 11 {
		t.Fatalf("expected at least 11 events, got %d", len(timestamps))
	}

	// All spinner events (indices 1-10) should share the same timestamp
	spinnerStart := timestamps[1]
	for i := 2; i <= 10; i++ {
		if timestamps[i] != spinnerStart {
			t.Errorf("spinner event %d has timestamp %.6f, expected same as first spinner (%.6f)", i, timestamps[i], spinnerStart)
			break
		}
	}

	// The first real content event (index 11) should advance beyond the spinner timestamp
	if len(timestamps) > 11 && timestamps[11] <= spinnerStart {
		t.Errorf("first real content event should advance past spinner time, got %.6f <= %.6f", timestamps[11], spinnerStart)
	}
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
