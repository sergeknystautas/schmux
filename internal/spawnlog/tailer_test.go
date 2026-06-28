package spawnlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTailerEmitsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spawn.jsonl")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	lines := make(chan string, 4)
	tailer, err := NewTailer(path, info.Size(), func(b []byte) { lines <- string(b) })
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer tailer.Stop()

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("second\n")
	f.Close()

	select {
	case got := <-lines:
		if got != "second" {
			t.Errorf("got %q, want second", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for appended line")
	}
}

// TestNewTailerCreatesMissingLogDir covers a fresh install: the logs dir does not
// exist yet (Append creates it on the first spawn). NewTailer must create it so
// opening /logs before any spawn does not fail and close the connection.
func TestNewTailerCreatesMissingLogDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "spawn.jsonl") // parent dir absent

	lines := make(chan string, 4)
	tailer, err := NewTailer(path, 0, func(b []byte) { lines <- string(b) })
	if err != nil {
		t.Fatalf("NewTailer should create the missing dir, got: %v", err)
	}
	defer tailer.Stop()

	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-lines:
		if got != "hello" {
			t.Errorf("got %q, want hello", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out after dir was auto-created")
	}
}

// TestTailerCatchesUpGapLine covers the backlog->watcher gap: a line appended
// after the caller's offset snapshot but before the tailer starts must be
// delivered via the initial catch-up, not held until the next append.
func TestTailerCatchesUpGapLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spawn.jsonl")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	// Append in the gap (after the offset snapshot, before NewTailer).
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("second\n")
	f.Close()

	lines := make(chan string, 4)
	tailer, err := NewTailer(path, info.Size(), func(b []byte) { lines <- string(b) })
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer tailer.Stop()

	select {
	case got := <-lines:
		if got != "second" {
			t.Errorf("got %q, want second", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out; gap line was not caught up")
	}
}
