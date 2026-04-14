package timelapse

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestRecording(t *testing.T, dir, id, sessionID string, startTime time.Time, duration float64) string {
	t.Helper()
	path := filepath.Join(dir, id+".cast")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Write asciicast v2 header
	header := fmt.Sprintf(`{"version":2,"width":80,"height":24,"timestamp":%d,"env":{"TERM":"xterm-256color"}}`,
		startTime.Unix())
	fmt.Fprintln(f, header)

	// Write an output event
	fmt.Fprintf(f, "[%.6f,\"o\",\"test output\"]\n", 0.0)

	// Write a final output event at the given duration
	fmt.Fprintf(f, "[%.6f,\"o\",\"final\"]\n", duration)

	f.Close()
	return path
}

func TestListRecordings(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	createTestRecording(t, dir, "s1-1001", "s1", now.Add(-2*time.Hour), 120.0)
	createTestRecording(t, dir, "s2-1002", "s2", now.Add(-1*time.Hour), 60.0)

	recordings, err := ListRecordings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recordings) != 2 {
		t.Fatalf("expected 2 recordings, got %d", len(recordings))
	}
	// Should be sorted newest first
	if recordings[0].RecordingID != "s2-1002" {
		t.Errorf("first recording = %q, want s2-1002", recordings[0].RecordingID)
	}
	if recordings[1].RecordingID != "s1-1001" {
		t.Errorf("second recording = %q, want s1-1001", recordings[1].RecordingID)
	}
	if recordings[0].InProgress {
		t.Error("recording should not be in-progress (has output events)")
	}
	if recordings[0].Duration != 60.0 {
		t.Errorf("duration = %f, want 60.0", recordings[0].Duration)
	}
}

func TestListRecordings_SkipsCompressedFiles(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	createTestRecording(t, dir, "s1-1001", "s1", now.Add(-1*time.Hour), 60.0)

	// Create a compressed file that should be skipped
	compressedPath := filepath.Join(dir, "s1-1001.timelapse.cast")
	os.WriteFile(compressedPath, []byte(`{"version":2,"width":80,"height":24}`+"\n"), 0600)

	recordings, err := ListRecordings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording (compressed should be skipped), got %d", len(recordings))
	}
	if recordings[0].RecordingID != "s1-1001" {
		t.Errorf("recording = %q, want s1-1001", recordings[0].RecordingID)
	}
}

func TestListRecordings_HasCompressed(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	createTestRecording(t, dir, "s1-1001", "s1", now.Add(-1*time.Hour), 60.0)

	// Initially, no compressed file
	recordings, _ := ListRecordings(dir)
	if recordings[0].HasCompressed {
		t.Error("should not have compressed file yet")
	}

	// Create compressed file
	compressedPath := filepath.Join(dir, "s1-1001.timelapse.cast")
	os.WriteFile(compressedPath, []byte(`{"version":2,"width":80,"height":24}`+"\n"), 0600)

	recordings, _ = ListRecordings(dir)
	if !recordings[0].HasCompressed {
		t.Error("should have compressed file")
	}
}

func TestListRecordings_HeaderOnlyIsInProgress(t *testing.T) {
	dir := t.TempDir()

	// Write a .cast file with only the header line (no output events).
	// This simulates a session that was disposed before producing output.
	headerOnly := filepath.Join(dir, "s1-1001.cast")
	os.WriteFile(headerOnly, []byte(
		fmt.Sprintf(`{"version":2,"width":80,"height":24,"timestamp":%d,"env":{"TERM":"xterm-256color"}}`, time.Now().Unix())+"\n",
	), 0600)

	recordings, err := ListRecordings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(recordings))
	}
	if !recordings[0].InProgress {
		t.Error("header-only recording should be marked InProgress (no output events yet)")
	}
}

func TestParseRecordingInfo_SessionIDDerivation(t *testing.T) {
	dir := t.TempDir()

	// New format: recording ID == session ID (no timestamp suffix).
	createTestRecording(t, dir, "schmux-002-abc12345", "", time.Now(), 10.0)
	info, err := parseRecordingInfo(filepath.Join(dir, "schmux-002-abc12345.cast"))
	if err != nil {
		t.Fatal(err)
	}
	if info.SessionID != "schmux-002-abc12345" {
		t.Errorf("new format: SessionID = %q, want %q", info.SessionID, "schmux-002-abc12345")
	}

	// Legacy format: <sessionID>-<unixTimestamp> (10+ digit suffix).
	createTestRecording(t, dir, "schmux-002-abc12345-1776201947", "", time.Now(), 10.0)
	info, err = parseRecordingInfo(filepath.Join(dir, "schmux-002-abc12345-1776201947.cast"))
	if err != nil {
		t.Fatal(err)
	}
	if info.SessionID != "schmux-002-abc12345" {
		t.Errorf("legacy format: SessionID = %q, want %q", info.SessionID, "schmux-002-abc12345")
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := createTestRecording(t, dir, "perm-test", "s1", time.Now(), 10.0)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}
}
