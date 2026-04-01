package timelapse

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestRecording(t *testing.T, dir, id, sessionID string, startTime time.Time, duration float64) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	WriteRecord(f, Record{
		Type:        RecordHeader,
		Version:     1,
		RecordingID: id,
		SessionID:   sessionID,
		StartTime:   startTime.Format(time.RFC3339),
		Width:       80,
		Height:      24,
	})
	WriteRecord(f, Record{
		Type: RecordOutput,
		T:    floatPtr(0),
		Seq:  0,
		D:    "test output",
	})
	WriteRecord(f, Record{
		Type: RecordEnd,
		T:    floatPtr(duration),
	})
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
	if recordings[0].SessionID != "s2" {
		t.Errorf("sessionID = %q, want s2", recordings[0].SessionID)
	}
	if recordings[0].InProgress {
		t.Error("recording should be complete (has end record)")
	}
	if recordings[0].Duration != 60.0 {
		t.Errorf("duration = %f, want 60.0", recordings[0].Duration)
	}
}

func TestPruneRecordings_AgeBased(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	// Old recording (10 days ago)
	oldPath := createTestRecording(t, dir, "old-1", "s1", now.AddDate(0, 0, -10), 100.0)
	// Recent recording (1 day ago)
	recentPath := createTestRecording(t, dir, "new-1", "s2", now.AddDate(0, 0, -1), 50.0)

	err := PruneRecordings(dir, 7, 1<<30) // 7 day retention, huge size limit
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old recording should be deleted")
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Error("recent recording should still exist")
	}
}

func TestPruneRecordings_SizeBased(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	// Create 3 recordings (each ~100 bytes)
	createTestRecording(t, dir, "r1-1", "s1", now.Add(-3*time.Hour), 10.0)
	createTestRecording(t, dir, "r2-2", "s2", now.Add(-2*time.Hour), 10.0)
	createTestRecording(t, dir, "r3-3", "s3", now.Add(-1*time.Hour), 10.0)

	// Set max total to very small (should evict oldest)
	err := PruneRecordings(dir, 365, 200) // 200 bytes max total
	if err != nil {
		t.Fatal(err)
	}

	remaining, _ := ListRecordings(dir)
	// Should have evicted at least the oldest
	if len(remaining) >= 3 {
		t.Errorf("expected fewer than 3 recordings after pruning, got %d", len(remaining))
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
